// Package wsconn provides a reconnecting WebSocket client for daemon-to-server communication.
package wsconn

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	defaultReconnectBase = 1 * time.Second
	defaultReconnectMax  = 60 * time.Second
	defaultDialTimeout   = 10 * time.Second
	defaultWriteTimeout  = 5 * time.Second
	backoffMultiplier    = 2
)

// Option configures the Client.
type Option func(*Client)

// WithReconnect sets the base and maximum delay for exponential backoff reconnection.
func WithReconnect(base, maximum time.Duration) Option {
	return func(c *Client) {
		c.reconnectBase = base
		c.reconnectMax = maximum
	}
}

// WithLogger sets a structured logger for connection lifecycle events.
func WithLogger(log *slog.Logger) Option {
	return func(c *Client) {
		c.log = log
	}
}

// Client maintains a persistent WebSocket connection with automatic
// reconnection. A single connect-or-retry loop establishes the initial
// connection and all subsequent reconnections through the same code path:
// there is no distinct "initial connect" that can fail terminally. A transient
// failure (DNS not ready at boot, server unreachable, network drop) is retried
// in-process with exponential backoff until it succeeds or the client is
// stopped — it never surfaces as a fatal error to the caller.
type Client struct {
	serverURL string
	token     string
	log       *slog.Logger

	mu             sync.Mutex
	conn           *websocket.Conn
	cancel         context.CancelFunc
	connected      chan struct{}
	forceReconnect chan struct{}

	messages  chan []byte
	closeOnce sync.Once
	wg        sync.WaitGroup

	reconnectBase time.Duration
	reconnectMax  time.Duration
	dialTimeout   time.Duration
	writeTimeout  time.Duration
}

// New creates a WebSocket Client targeting the given server URL.
func New(serverURL, token string, opts ...Option) *Client {
	client := &Client{
		serverURL:      serverURL,
		token:          token,
		log:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		messages:       make(chan []byte, 64),
		connected:      make(chan struct{}, 1),
		forceReconnect: make(chan struct{}, 1),
		reconnectBase:  defaultReconnectBase,
		reconnectMax:   defaultReconnectMax,
		dialTimeout:    defaultDialTimeout,
		writeTimeout:   defaultWriteTimeout,
	}
	for _, opt := range opts {
		opt(client)
	}
	return client
}

// IsConnected reports whether the WebSocket connection is currently established.
func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn != nil
}

// Start launches the connect loop in a background goroutine and returns
// immediately. The loop dials with exponential backoff until connected, serves
// the connection until it drops, then redials — for the life of the client.
// Start never blocks on or fails from a dial; connection availability is
// observed via Connected() and IsConnected().
//
// Shutdown is driven entirely by context cancellation: Start derives a
// cancelable context, and Close cancels it. Every blocking operation (Read,
// backoff sleep, dial) is tied to that context, so Close reliably unblocks the
// loop even mid-reconnect — without depending on catching the live connection
// pointer at the right instant.
func (c *Client) Start(ctx context.Context) {
	//nolint:gosec // G118: cancel is stored in c.cancel and called in Close.
	ctx, cancel := context.WithCancel(ctx)

	c.mu.Lock()
	c.cancel = cancel
	c.mu.Unlock()

	c.wg.Go(func() { c.connectLoop(ctx) })
}

// connectLoop is the single path for establishing and re-establishing the
// connection. The first attempt is immediate; every attempt after a failure or
// a drop waits a backoff delay (which ForceReconnect can skip).
func (c *Client) connectLoop(ctx context.Context) {
	var delay time.Duration // zero => first attempt is immediate
	for {
		if delay > 0 && !c.waitBackoff(ctx, delay) {
			return
		}
		if c.stopping(ctx) {
			return
		}

		conn, err := c.dial(ctx)
		if err != nil {
			c.log.WarnContext(ctx, "websocket dial failed, retrying",
				slog.Duration("delay", delay), slog.String("error", err.Error()))
			delay = nextDelay(delay, c.reconnectBase, c.reconnectMax)
			continue
		}

		c.mu.Lock()
		c.conn = conn
		c.mu.Unlock()
		c.log.InfoContext(ctx, "websocket connected", slog.String("url", c.serverURL))
		c.signalConnected()

		c.serveConn(ctx, conn)

		c.mu.Lock()
		c.conn = nil
		c.mu.Unlock()

		// After a drop, back off starting from the base delay before redialing.
		delay = c.reconnectBase
	}
}

// waitBackoff sleeps for delay, returning false if the client should stop
// (context canceled or Close called) and true if it should dial now (timer
// elapsed or a ForceReconnect signal skipped the wait).
func (c *Client) waitBackoff(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	c.log.WarnContext(ctx, "websocket disconnected, reconnecting", slog.Duration("delay", delay))
	select {
	case <-ctx.Done():
		return false
	case <-c.forceReconnect:
		c.log.InfoContext(ctx, "force reconnect signal received, skipping backoff")
		return true
	case <-timer.C:
		return true
	}
}

// serveConn reads messages from conn into the messages channel until a read
// error (connection drop, ForceReconnect close, or shutdown), then returns so
// connectLoop can redial.
func (c *Client) serveConn(ctx context.Context, conn *websocket.Conn) {
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		select {
		case c.messages <- data:
		case <-ctx.Done():
			return
		}
	}
}

// signalConnected notifies a consumer that a connection was (re)established.
// Drain-then-send keeps the buffered (size 1) signal current without blocking.
func (c *Client) signalConnected() {
	select {
	case <-c.connected:
	default:
	}
	c.connected <- struct{}{}
}

func nextDelay(current, base, maximum time.Duration) time.Duration {
	if current <= 0 {
		return base
	}
	return min(current*backoffMultiplier, maximum)
}

// Send writes a message to the WebSocket. Returns nil if disconnected (message dropped).
func (c *Client) Send(msg []byte) error {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()

	if conn == nil {
		c.log.Debug("ws send dropped, not connected")
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.writeTimeout)
	defer cancel()

	if err := conn.Write(ctx, websocket.MessageBinary, msg); err != nil {
		return fmt.Errorf("ws write: %w", err)
	}
	return nil
}

// Messages returns the channel of incoming messages from the server.
func (c *Client) Messages() <-chan []byte { return c.messages }

// Connected returns a channel that signals after each successful connection,
// including the first. The channel is buffered (size 1) so a signal is never
// lost, but only the most recent connection is retained if the consumer is slow.
func (c *Client) Connected() <-chan struct{} { return c.connected }

// ForceReconnect closes the current connection (if any) and signals the connect
// loop to skip its backoff delay. Use this after events like resume-from-sleep
// where the connection is dead but the process is alive. Safe to call multiple
// times rapidly — the signal is buffered and non-blocking.
func (c *Client) ForceReconnect() {
	c.log.Info("force reconnect requested")

	c.mu.Lock()
	if c.conn != nil {
		if closeErr := c.conn.Close(websocket.StatusGoingAway, "force reconnect"); closeErr != nil {
			c.log.Debug("close for force reconnect failed", slog.String("error", closeErr.Error()))
		}
		// serveConn holds a local copy of conn and will get a read error from
		// the closed connection, returning so connectLoop redials.
	}
	c.mu.Unlock()

	// Non-blocking send — if one signal is already pending, skip.
	select {
	case c.forceReconnect <- struct{}{}:
	default:
	}
}

// Close shuts down the client, stopping the connect loop and closing the
// connection. Canceling the loop context is what reliably unblocks the loop;
// the explicit connection Close is a best-effort clean closure handshake.
func (c *Client) Close() error {
	c.log.Debug("websocket closing")
	var closeErr error
	c.closeOnce.Do(func() {
		c.mu.Lock()
		if c.cancel != nil {
			c.cancel()
		}
		if c.conn != nil {
			closeErr = c.conn.Close(websocket.StatusNormalClosure, "shutdown")
			c.conn = nil
		}
		c.mu.Unlock()
	})

	c.wg.Wait()

	if closeErr != nil {
		return fmt.Errorf("ws close: %w", closeErr)
	}
	return nil
}

func (c *Client) dial(ctx context.Context) (*websocket.Conn, error) {
	dialCtx, cancel := context.WithTimeout(ctx, c.dialTimeout)
	defer cancel()

	conn, resp, err := websocket.Dial(dialCtx, c.serverURL, &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Authorization": {"Bearer " + c.token},
		},
	})
	if resp != nil && resp.Body != nil {
		if closeErr := resp.Body.Close(); closeErr != nil {
			c.log.DebugContext(ctx, "close response body failed", slog.String("error", closeErr.Error()))
		}
	}
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", c.serverURL, err)
	}
	return conn, nil
}

// stopping reports whether the connect loop should exit. Shutdown (Close) and
// caller cancellation both cancel the loop context.
func (c *Client) stopping(ctx context.Context) bool {
	return ctx.Err() != nil
}
