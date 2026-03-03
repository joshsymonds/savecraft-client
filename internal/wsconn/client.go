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

// Client maintains a persistent WebSocket connection with automatic reconnection.
// It satisfies the daemon.WSClient interface.
type Client struct {
	serverURL string
	token     string
	log       *slog.Logger

	mu          sync.Mutex
	conn        *websocket.Conn
	connReady   chan struct{}
	reconnected chan struct{}

	messages  chan []byte
	done      chan struct{}
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
		serverURL:     serverURL,
		token:         token,
		log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		messages:      make(chan []byte, 64),
		reconnected:   make(chan struct{}, 1),
		done:          make(chan struct{}),
		reconnectBase: defaultReconnectBase,
		reconnectMax:  defaultReconnectMax,
		dialTimeout:   defaultDialTimeout,
		writeTimeout:  defaultWriteTimeout,
	}
	for _, opt := range opts {
		opt(client)
	}
	return client
}

// Connected reports whether the WebSocket connection is currently established.
func (c *Client) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn != nil
}

// Connect establishes the initial WebSocket connection and starts the read loop.
func (c *Client) Connect(ctx context.Context) error {
	conn, err := c.dialWS(ctx)
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.conn = conn
	c.connReady = make(chan struct{})
	close(c.connReady)
	c.mu.Unlock()

	c.log.InfoContext(ctx, "websocket connected", slog.String("url", c.serverURL))
	c.wg.Go(c.readLoop)

	return nil
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

	if err := conn.Write(ctx, websocket.MessageText, msg); err != nil {
		return fmt.Errorf("ws write: %w", err)
	}
	return nil
}

// Messages returns the channel of incoming messages from the server.
func (c *Client) Messages() <-chan []byte { return c.messages }

// Reconnected returns a channel that signals after each successful reconnection.
// The channel is buffered (size 1) so a signal is never lost, but only the most
// recent reconnect is retained if the consumer is slow.
func (c *Client) Reconnected() <-chan struct{} { return c.reconnected }

// Close shuts down the client, closing the connection and stopping the read loop.
func (c *Client) Close() error {
	c.log.Debug("websocket closing")
	var closeErr error
	c.closeOnce.Do(func() {
		close(c.done)

		c.mu.Lock()
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

func (c *Client) readLoop() {
	defer c.cleanupConn()

	for {
		if c.isClosed() {
			return
		}

		c.mu.Lock()
		conn := c.conn
		ready := c.connReady
		c.mu.Unlock()

		if conn == nil {
			select {
			case <-ready:
				continue
			case <-c.done:
				return
			}
		}

		_, data, err := conn.Read(context.Background())
		if err != nil {
			if c.isClosed() {
				return
			}
			c.reconnect()
			continue
		}

		select {
		case c.messages <- data:
		case <-c.done:
			return
		}
	}
}

func (c *Client) reconnect() {
	c.mu.Lock()
	if c.conn != nil {
		if closeErr := c.conn.Close(websocket.StatusGoingAway, "reconnecting"); closeErr != nil {
			c.log.Debug("close before reconnect failed", slog.String("error", closeErr.Error()))
		}
		c.conn = nil
	}
	c.connReady = make(chan struct{})
	c.mu.Unlock()

	delay := c.reconnectBase
	attempts := 0
	for {
		c.log.Warn("websocket disconnected, reconnecting", slog.Duration("delay", delay))
		timer := time.NewTimer(delay)
		select {
		case <-c.done:
			timer.Stop()
			return
		case <-timer.C:
		}

		if c.isClosed() {
			return
		}

		attempts++
		ctx, cancel := context.WithTimeout(context.Background(), c.dialTimeout)
		conn, err := c.dialWS(ctx)
		cancel()

		if err != nil {
			c.log.Warn("reconnect failed", slog.Duration("delay", delay), slog.String("error", err.Error()))
			delay = min(delay*backoffMultiplier, c.reconnectMax)
			continue
		}

		c.mu.Lock()
		c.conn = conn
		close(c.connReady)
		c.mu.Unlock()
		c.log.Info("websocket reconnected", slog.Int("attempts", attempts))

		// Drain-then-send so the signal is never lost but never blocks.
		select {
		case <-c.reconnected:
		default:
		}
		c.reconnected <- struct{}{}

		return
	}
}

func (c *Client) dialWS(ctx context.Context) (*websocket.Conn, error) {
	conn, resp, err := websocket.Dial(ctx, c.serverURL, &websocket.DialOptions{
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

func (c *Client) cleanupConn() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		if closeErr := c.conn.Close(websocket.StatusNormalClosure, "shutdown"); closeErr != nil {
			c.log.Debug("close on shutdown failed", slog.String("error", closeErr.Error()))
		}
		c.conn = nil
	}
}

func (c *Client) isClosed() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}
