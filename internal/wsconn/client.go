// Package wsconn provides a reconnecting WebSocket client for daemon-to-server communication.
package wsconn

import (
	"context"
	"fmt"
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

// Client maintains a persistent WebSocket connection with automatic reconnection.
// It satisfies the daemon.WSClient interface.
type Client struct {
	serverURL string
	token     string

	mu   sync.Mutex
	conn *websocket.Conn

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
		messages:      make(chan []byte, 64),
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

// Connect establishes the initial WebSocket connection and starts the read loop.
func (c *Client) Connect(ctx context.Context) error {
	conn, err := c.dialWS(ctx)
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	c.wg.Go(c.readLoop)

	return nil
}

// Send writes a message to the WebSocket. Returns nil if disconnected (message dropped).
func (c *Client) Send(msg []byte) error {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()

	if conn == nil {
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

// Close shuts down the client, closing the connection and stopping the read loop.
func (c *Client) Close() error {
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
		c.mu.Unlock()

		if conn == nil {
			select {
			case <-time.After(10 * time.Millisecond):
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
		_ = c.conn.Close(websocket.StatusGoingAway, "reconnecting")
		c.conn = nil
	}
	c.mu.Unlock()

	delay := c.reconnectBase
	for {
		select {
		case <-c.done:
			return
		case <-time.After(delay):
		}

		if c.isClosed() {
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), c.dialTimeout)
		conn, err := c.dialWS(ctx)
		cancel()

		if err != nil {
			delay = min(delay*backoffMultiplier, c.reconnectMax)
			continue
		}

		c.mu.Lock()
		c.conn = conn
		c.mu.Unlock()
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
		_ = resp.Body.Close()
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
		_ = c.conn.Close(websocket.StatusNormalClosure, "shutdown")
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
