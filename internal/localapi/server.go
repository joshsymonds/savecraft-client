package localapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Server is the daemon's local HTTP API server.
// It serves boot status and link info on localhost, and accepts
// additional handlers (e.g. /status) via Handle.
type Server struct {
	mu        sync.RWMutex
	state     State
	linkCode  string
	linkURL   string
	expiresAt string
	errMsg    string

	mux    *http.ServeMux
	srv    *http.Server
	logger *slog.Logger
}

// NewServer creates a local API server bound to addr.
// Pass nil logger for no-op logging.
func NewServer(addr string, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(discardW{}, nil))
	}

	server := &Server{
		state:  StateStarting,
		mux:    http.NewServeMux(),
		logger: logger,
	}
	server.mux.HandleFunc("/boot", server.handleBoot)
	server.mux.HandleFunc("/link", server.handleLink)
	server.srv = &http.Server{
		Addr:              addr,
		Handler:           server.mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	return server
}

type discardW struct{}

func (discardW) Write(p []byte) (int, error) { return len(p), nil }

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
	if status != http.StatusOK {
		w.WriteHeader(status)
	}

	if err := json.NewEncoder(w).Encode(body); err != nil {
		s.logger.Error("write JSON response", slog.String("error", err.Error()))
	}
}

func (s *Server) handleBoot(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	resp := BootResponse{State: s.state}
	if s.errMsg != "" {
		resp.Error = s.errMsg
	}

	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleLink(w http.ResponseWriter, _ *http.Request) {
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
		s.writeJSON(w, http.StatusNotFound, LinkResponse{Error: "device was already registered"})

		return
	}

	s.writeJSON(w, http.StatusServiceUnavailable, LinkResponse{
		Error: "device not yet registered",
		State: s.state,
	})
}
