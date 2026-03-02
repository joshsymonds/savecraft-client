package daemon

import "time"

// GameStatusInfo describes the live state of a configured game.
type GameStatusInfo struct {
	SavePath       string   `json:"savePath"`
	Enabled        bool     `json:"enabled"`
	Watching       bool     `json:"watching"`
	FileExtensions []string `json:"fileExtensions"`
}

// DaemonStatus is a snapshot of the daemon's live state, suitable for
// JSON serialization by the diagnostic HTTP endpoint.
type DaemonStatus struct {
	Uptime      string                    `json:"uptime"`
	Version     string                    `json:"version"`
	DeviceID    string                    `json:"deviceId"`
	WSConnected bool                      `json:"wsConnected"`
	Games       map[string]GameStatusInfo `json:"games"`
}

// Status returns a snapshot of the daemon's current state.
// Safe to call from any goroutine (uses RLock).
func (d *Daemon) Status() DaemonStatus {
	d.mu.RLock()
	defer d.mu.RUnlock()

	games := make(map[string]GameStatusInfo, len(d.cfg.Games))
	for gameID, cfg := range d.cfg.Games {
		_, watching := d.watchedDirs[cfg.SavePath]
		games[gameID] = GameStatusInfo{
			SavePath:       cfg.SavePath,
			Enabled:        cfg.Enabled,
			Watching:       watching,
			FileExtensions: cfg.FileExtensions,
		}
	}

	return DaemonStatus{
		Uptime:      time.Since(d.startTime).Truncate(time.Second).String(),
		Version:     d.cfg.Version,
		DeviceID:    d.cfg.DeviceID,
		WSConnected: d.ws.Connected(),
		Games:       games,
	}
}
