package wsconn

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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

func connectClient(t *testing.T, serverURL string, opts ...Option) *Client {
	t.Helper()
	client := New(serverURL, "token", opts...)
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { client.Close() })
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

func TestConnect_Success(t *testing.T) {
	srv := echoServer(t)
	_ = connectClient(t, wsURL(srv.URL))
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

	client := New(wsURL(srv.URL), "token")
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
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
	// Retry Send because the client's reconnect() may not have set c.conn yet
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

func TestClose_Idempotent(t *testing.T) {
	srv := echoServer(t)

	client := New(wsURL(srv.URL), "token")
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	if err := client.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestConnect_BadURL(t *testing.T) {
	client := New("ws://127.0.0.1:1/ws/daemon", "token")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := client.Connect(ctx)
	if err == nil {
		client.Close()
		t.Fatal("expected error for unreachable server")
	}
}
