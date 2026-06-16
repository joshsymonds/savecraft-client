package wsconn

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func wsURL(httpURL string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http")
}

func echoServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		for {
			typ, data, readErr := conn.Read(r.Context())
			if readErr != nil {
				return
			}
			if writeErr := conn.Write(r.Context(), typ, data); writeErr != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// waitForConnected starts the client and blocks until its first connection is
// established (signaled on Connected()), failing the test on timeout.
func waitForConnected(t *testing.T, client *Client) {
	t.Helper()
	select {
	case <-client.Connected():
	case <-time.After(2 * time.Second):
		t.Fatal("client did not connect")
	}
}

func connectClient(t *testing.T, serverURL string, opts ...Option) *Client {
	t.Helper()
	client := New(serverURL, "token", opts...)
	client.Start(context.Background())
	t.Cleanup(func() { client.Close() })
	waitForConnected(t, client)
	return client
}

func waitForMsg(t *testing.T, ch <-chan []byte) []byte {
	t.Helper()
	select {
	case msg := <-ch:
		return msg
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for message")
		return nil
	}
}

func TestStart_Connects(t *testing.T) {
	srv := echoServer(t)
	_ = connectClient(t, wsURL(srv.URL))
}

// TestConnected_SignalsOnFirstConnect is the symmetry guard: the Connected()
// channel must fire on the FIRST successful connection, not only on
// reconnects. The old Connect/reconnect split signaled only on reconnect.
func TestConnected_SignalsOnFirstConnect(t *testing.T) {
	srv := echoServer(t)

	client := New(wsURL(srv.URL), "token")
	client.Start(context.Background())
	t.Cleanup(func() { client.Close() })

	select {
	case <-client.Connected():
		// expected: the initial connection signals.
	case <-time.After(2 * time.Second):
		t.Fatal("Connected() did not signal on the first connection")
	}
}

// TestStart_RetriesUntilConnected is the core regression guard for the
// transient-as-fatal bug: an initial dial failure must NOT be fatal — the
// client retries in-process and connects once the server is reachable.
func TestStart_RetriesUntilConnected(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Reject the first two dials (non-101 responses fail websocket.Dial),
		// then accept — simulating a server/network that isn't ready yet.
		if attempts.Add(1) < 3 {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		for {
			if _, _, readErr := conn.Read(r.Context()); readErr != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)

	client := New(wsURL(srv.URL), "token", WithReconnect(10*time.Millisecond, 50*time.Millisecond))
	client.Start(context.Background())
	t.Cleanup(func() { client.Close() })

	select {
	case <-client.Connected():
		// expected: connected after retrying past the early failures.
	case <-time.After(3 * time.Second):
		t.Fatal("client never connected despite retrying past initial failures")
	}

	if got := attempts.Load(); got < 3 {
		t.Errorf("expected the client to retry (>=3 dial attempts), got %d", got)
	}
}

func TestSendAndReceive(t *testing.T) {
	srv := echoServer(t)
	client := connectClient(t, wsURL(srv.URL))

	if err := client.Send([]byte(`{"type":"test"}`)); err != nil {
		t.Fatalf("Send: %v", err)
	}

	msg := waitForMsg(t, client.Messages())
	if string(msg) != `{"type":"test"}` {
		t.Errorf("got %q, want {\"type\":\"test\"}", string(msg))
	}
}

func TestMessages_FromServer(t *testing.T) {
	ready := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		<-ready
		conn.Write(r.Context(), websocket.MessageText, []byte(`{"command":"rescan"}`))
		conn.Read(r.Context()) // keep alive
	}))
	t.Cleanup(srv.Close)

	client := connectClient(t, wsURL(srv.URL))
	close(ready)

	msg := waitForMsg(t, client.Messages())
	if string(msg) != `{"command":"rescan"}` {
		t.Errorf("got %q", string(msg))
	}
}

func TestSend_DropsWhenClosed(t *testing.T) {
	srv := echoServer(t)

	client := connectClient(t, wsURL(srv.URL))
	client.Close()

	if err := client.Send([]byte("hello")); err != nil {
		t.Errorf("Send after Close should return nil, got: %v", err)
	}
}

func TestReconnect_AfterServerClose(t *testing.T) {
	conns := make(chan *websocket.Conn, 5)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		conns <- conn
		for {
			typ, data, readErr := conn.Read(r.Context())
			if readErr != nil {
				return
			}
			if writeErr := conn.Write(r.Context(), typ, data); writeErr != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)

	client := connectClient(t, wsURL(srv.URL),
		WithReconnect(10*time.Millisecond, 50*time.Millisecond),
	)

	// First connection established.
	conn1 := <-conns
	conn1.Close(websocket.StatusGoingAway, "test disconnect")

	// Wait for automatic reconnect.
	select {
	case <-conns:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for reconnect")
	}

	// Verify functional on new connection via echo roundtrip.
	// Retry Send because the connect loop may not have set c.conn yet
	// even though the server already accepted (and pushed to conns).
	deadline := time.After(2 * time.Second)
	for {
		client.Send([]byte(`{"after":"reconnect"}`))
		select {
		case msg := <-client.Messages():
			if string(msg) != `{"after":"reconnect"}` {
				t.Errorf("got %q", string(msg))
			}
			return
		case <-time.After(50 * time.Millisecond):
		case <-deadline:
			t.Fatal("timed out waiting for echo after reconnect")
		}
	}
}

// TestConnected_SignalsAfterReconnect verifies the Connected() channel fires
// again after a dropped connection is re-established (not only on first
// connect).
func TestConnected_SignalsAfterReconnect(t *testing.T) {
	conns := make(chan *websocket.Conn, 5)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		conns <- conn
		for {
			_, _, readErr := conn.Read(r.Context())
			if readErr != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)

	// connectClient consumes the first Connected() signal.
	client := connectClient(t, wsURL(srv.URL),
		WithReconnect(10*time.Millisecond, 50*time.Millisecond),
	)

	// Kill first connection to trigger reconnect.
	conn1 := <-conns
	conn1.Close(websocket.StatusGoingAway, "test disconnect")

	// Connected() should signal again after the reconnect.
	select {
	case <-client.Connected():
		// expected
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Connected() signal after reconnect")
	}

	// Confirm a second server-side accept happened.
	select {
	case <-conns:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for second connection")
	}
}

func TestForceReconnect_TriggersImmediateReconnect(t *testing.T) {
	conns := make(chan *websocket.Conn, 5)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		conns <- conn
		for {
			_, _, readErr := conn.Read(r.Context())
			if readErr != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)

	// Use a long reconnect base so that without ForceReconnect, the test
	// would time out waiting for the backoff delay.
	client := connectClient(t, wsURL(srv.URL),
		WithReconnect(30*time.Second, 30*time.Second),
	)

	// Consume initial connection.
	<-conns

	// Force reconnect — should close current conn and reconnect immediately,
	// NOT wait for the 30s backoff.
	client.ForceReconnect()

	select {
	case <-conns:
		// Reconnected within a reasonable time — ForceReconnect bypassed backoff.
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for reconnect — ForceReconnect did not bypass backoff")
	}

	// Verify Connected() channel signals after the forced reconnect.
	select {
	case <-client.Connected():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Connected() signal after ForceReconnect")
	}
}

func TestForceReconnect_Idempotent(t *testing.T) {
	conns := make(chan *websocket.Conn, 5)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		conns <- conn
		for {
			_, _, readErr := conn.Read(r.Context())
			if readErr != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)

	client := connectClient(t, wsURL(srv.URL),
		WithReconnect(30*time.Second, 30*time.Second),
	)
	<-conns

	// Multiple rapid calls should not panic or deadlock.
	client.ForceReconnect()
	client.ForceReconnect()
	client.ForceReconnect()

	// Should still reconnect.
	select {
	case <-conns:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for reconnect after multiple ForceReconnect calls")
	}
}

func TestClose_Idempotent(t *testing.T) {
	srv := echoServer(t)

	client := connectClient(t, wsURL(srv.URL))

	if err := client.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}
