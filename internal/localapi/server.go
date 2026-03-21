package localapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Server is the daemon's local HTTP API server.
// It serves boot status, link info, logs, and control endpoints on localhost.
type Server struct {
	mu        sync.RWMutex
	state     State
	linkCode  string
	linkURL   string
	expiresAt string
	errMsg    string

	ringBuf          *RingBuffer
	shutdownFn       func()
	restartFn        func() error
	repairFn         func(ctx context.Context) (linkCode, linkURL, expiresAt string, err error)
	updatePluginsFn  func(ctx context.Context) ([]string, error)
	pendingVersionFn func() string

	mux    *http.ServeMux
	srv    *http.Server
	logger *slog.Logger
}

// NewServer creates a local API server bound to addr.
// Pass nil logger for no-op logging.
func NewServer(addr string, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	server := &Server{
		state:  StateStarting,
		mux:    http.NewServeMux(),
		logger: logger,
	}
	server.mux.HandleFunc("/boot", server.handleBoot)
	server.mux.HandleFunc("/link", server.handleLink)
	server.mux.HandleFunc("/logs", server.handleLogs)
	server.mux.HandleFunc("/shutdown", server.handleShutdown)
	server.mux.HandleFunc("/restart", server.handleRestart)
	server.mux.HandleFunc("/repair", server.handleRepair)
	server.mux.HandleFunc("/update-plugins", server.handleUpdatePlugins)
	server.srv = &http.Server{
		Addr:              addr,
		Handler:           server.mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	return server
}

// Start begins serving in a background goroutine.
func (s *Server) Start() {
	go func() {
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("local API server failed", slog.String("error", err.Error()))
		}
	}()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	if err := s.srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("local API shutdown: %w", err)
	}

	return nil
}

// Handle registers an additional handler on the server's mux.
func (s *Server) Handle(pattern string, handler http.Handler) {
	s.mux.Handle(pattern, handler)
}

// SetState updates the daemon's lifecycle state.
func (s *Server) SetState(state State) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state = state
}

// SetRegistered marks the daemon as registered with a link code.
func (s *Server) SetRegistered(linkCode, linkURL, expiresAt string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state = StateRegistered
	s.linkCode = linkCode
	s.linkURL = linkURL
	s.expiresAt = expiresAt
}

// SetError marks the daemon as errored with a message.
func (s *Server) SetError(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state = StateError
	s.errMsg = msg
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(body); err != nil {
		s.logger.Error("write JSON response", slog.String("error", err.Error()))
	}
}

func (s *Server) handleBoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		s.writeJSON(w, http.StatusMethodNotAllowed, OKResponse{Error: "use GET"})

		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	resp := BootResponse{State: s.state}
	if s.errMsg != "" {
		resp.Error = s.errMsg
	}
	if s.pendingVersionFn != nil {
		resp.PendingVersion = s.pendingVersionFn()
	}

	s.writeJSON(w, http.StatusOK, resp)
}

// SetRingBuffer sets the ring buffer used by GET /logs.
func (s *Server) SetRingBuffer(rb *RingBuffer) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ringBuf = rb
}

// SetShutdownFunc sets the callback invoked by POST /shutdown.
func (s *Server) SetShutdownFunc(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.shutdownFn = fn
}

// SetRestartFunc sets the callback invoked by POST /restart.
func (s *Server) SetRestartFunc(fn func() error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.restartFn = fn
}

// SetRepairFunc sets the callback invoked by POST /repair.
// The callback should unlink the source and start the wait-for-link flow,
// returning the new link code, URL, and expiry.
func (s *Server) SetRepairFunc(fn func(ctx context.Context) (linkCode, linkURL, expiresAt string, err error)) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.repairFn = fn
}

// SetUpdatePluginsFunc sets the callback invoked by POST /update-plugins.
func (s *Server) SetUpdatePluginsFunc(fn func(ctx context.Context) ([]string, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.updatePluginsFn = fn
}

// SetPendingVersionFunc sets the function called by GET /boot to check
// whether a daemon update is available. Returns the pending version string
// or "" if no update is pending.
func (s *Server) SetPendingVersionFunc(fn func() string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.pendingVersionFn = fn
}

func (s *Server) handleUpdatePlugins(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		s.writeJSON(w, http.StatusMethodNotAllowed, UpdatePluginsResponse{Error: "use POST"})

		return
	}

	s.mu.RLock()
	fn := s.updatePluginsFn
	s.mu.RUnlock()

	if fn == nil {
		s.writeJSON(w, http.StatusServiceUnavailable, UpdatePluginsResponse{Error: "plugin updates not available"})

		return
	}

	updated, err := fn(r.Context())
	if err != nil {
		s.logger.Error("update plugins failed", slog.String("error", err.Error()))
		s.writeJSON(w, http.StatusInternalServerError, UpdatePluginsResponse{Error: "plugin update failed"})

		return
	}

	if updated == nil {
		updated = []string{}
	}

	s.writeJSON(w, http.StatusOK, UpdatePluginsResponse{Updated: updated})
}

func (s *Server) handleRepair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		s.writeJSON(w, http.StatusMethodNotAllowed, OKResponse{Error: "use POST"})

		return
	}

	s.mu.RLock()
	fn := s.repairFn
	s.mu.RUnlock()

	if fn == nil {
		s.writeJSON(w, http.StatusServiceUnavailable, LinkResponse{Error: "repair not available"})

		return
	}

	linkCode, linkURL, expiresAt, err := fn(r.Context())
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, LinkResponse{Error: err.Error()})

		return
	}

	s.writeJSON(w, http.StatusOK, LinkResponse{
		LinkCode:  linkCode,
		LinkURL:   linkURL,
		ExpiresAt: expiresAt,
	})
}

func (s *Server) handleLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		s.writeJSON(w, http.StatusMethodNotAllowed, OKResponse{Error: "use GET"})

		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.linkCode != "" {
		s.writeJSON(w, http.StatusOK, LinkResponse{
			LinkCode:  s.linkCode,
			LinkURL:   s.linkURL,
			ExpiresAt: s.expiresAt,
		})

		return
	}

	if s.state == StateRunning {
		s.writeJSON(w, http.StatusNotFound, LinkResponse{Error: "source was already registered"})

		return
	}

	s.writeJSON(w, http.StatusServiceUnavailable, LinkResponse{
		Error: "source not yet registered",
		State: s.state,
	})
}

func (s *Server) handleLogs(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	rb := s.ringBuf
	s.mu.RUnlock()

	if rb == nil {
		s.writeJSON(w, http.StatusServiceUnavailable, OKResponse{Error: "log buffer not available"})

		return
	}

	entries := rb.Entries()
	if entries == nil {
		entries = []LogEntry{}
	}

	s.writeJSON(w, http.StatusOK, LogsResponse{Entries: entries})
}

func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		s.writeJSON(w, http.StatusMethodNotAllowed, OKResponse{Error: "use POST"})

		return
	}

	s.mu.RLock()
	fn := s.shutdownFn
	s.mu.RUnlock()

	if fn == nil {
		s.writeJSON(w, http.StatusServiceUnavailable, OKResponse{Error: "shutdown not available"})

		return
	}

	s.writeJSON(w, http.StatusOK, OKResponse{OK: true})

	// Call shutdown after writing the response so the client gets a clean 200.
	go fn()
}

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		s.writeJSON(w, http.StatusMethodNotAllowed, OKResponse{Error: "use POST"})

		return
	}

	s.mu.RLock()
	restartFn := s.restartFn
	s.mu.RUnlock()

	if restartFn == nil {
		s.writeJSON(w, http.StatusServiceUnavailable, OKResponse{Error: "restart not available"})

		return
	}

	if err := restartFn(); err != nil {
		s.writeJSON(w, http.StatusInternalServerError, OKResponse{Error: err.Error()})

		return
	}

	s.writeJSON(w, http.StatusOK, OKResponse{OK: true})
}
