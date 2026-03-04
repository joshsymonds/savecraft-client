package cmd

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
)

// bootStatus tracks the daemon's boot progress and registration result.
// Thread-safe — updated by the boot goroutine, read by HTTP handlers.
type bootStatus struct {
	mu        sync.RWMutex
	state     string
	linkCode  string
	linkURL   string
	expiresAt string
	errMsg    string
}

func newBootStatus() *bootStatus {
	return &bootStatus{state: "starting"}
}

func (bs *bootStatus) setState(state string) {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	bs.state = state
}

func (bs *bootStatus) setRegistered(linkCode, linkURL, expiresAt string) {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	bs.state = "registered"
	bs.linkCode = linkCode
	bs.linkURL = linkURL
	bs.expiresAt = expiresAt
}

func (bs *bootStatus) setError(msg string) {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	bs.state = "error"
	bs.errMsg = msg
}

// bootHandler serves GET /boot — returns the daemon's boot state.
func (bs *bootStatus) bootHandler(rw http.ResponseWriter, _ *http.Request) {
	bs.mu.RLock()
	defer bs.mu.RUnlock()

	resp := map[string]string{"state": bs.state}
	if bs.errMsg != "" {
		resp["error"] = bs.errMsg
	}

	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(resp)
}

// linkHandler serves GET /link — returns the link code and clickable URL.
// Returns 503 while still registering, 404 if the daemon already had credentials.
func (bs *bootStatus) linkHandler(rw http.ResponseWriter, _ *http.Request) {
	bs.mu.RLock()
	defer bs.mu.RUnlock()

	if bs.linkCode == "" {
		if bs.state == "running" || bs.state == "registered" && bs.linkCode == "" {
			rw.Header().Set("Content-Type", "application/json")
			rw.WriteHeader(http.StatusNotFound)
			json.NewEncoder(rw).Encode(map[string]string{"error": "device was already registered"})

			return
		}

		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(rw).Encode(map[string]string{"error": "device not yet registered",
			"state": bs.state,
		})

		return
	}

	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(map[string]string{"linkCode": bs.linkCode,
		"linkUrl":   bs.linkURL,
		"expiresAt": bs.expiresAt,
	})
}

// buildLinkURL constructs the frontend link URL from the base URL and code.
func buildLinkURL(frontendURL, linkCode string) string {
	return strings.TrimRight(frontendURL, "/") + "/link/" + linkCode
}

// startBootServer starts the status HTTP server with /boot and /link endpoints.
// Returns the mux (for adding /status later) and the server (for shutdown).
func startBootServer(
	boot *bootStatus, addr string, logger *slog.Logger,
) (*http.ServeMux, *http.Server) {
	mux := http.NewServeMux()
	mux.HandleFunc("/boot", boot.bootHandler)
	mux.HandleFunc("/link", boot.linkHandler)

	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	go func() {
		if listenErr := srv.ListenAndServe(); listenErr != nil && listenErr != http.ErrServerClosed {
			logger.Error("status server failed", slog.String("error", listenErr.Error()))
		}
	}()

	return mux, srv
}
