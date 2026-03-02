package daemon

import (
	"encoding/json"
	"net/http"
)

// StatusHandler returns an HTTP handler that serves the daemon's live
// diagnostic state as JSON. Intended for localhost-only access.
func StatusHandler(d *Daemon) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(d.Status()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
}
