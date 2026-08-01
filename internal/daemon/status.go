package daemon

import "time"

// GameStatusInfo describes the live state of a configured game.
type GameStatusInfo struct {
	SavePath       string   `json:"savePath"`
	Enabled        bool     `json:"enabled"`
	Watching       bool     `json:"watching"`
	FileExtensions []string `json:"fileExtensions"`
	// SaveCount is the number of directories currently watched for this
	// game (d.watchedDirs entries whose value is this gameID). For a
	// directory-unit game each entry is one save directory, so this is the
	// live save count; for a file-unit game it is the number of watched
	// save directories, not individual save files.
	SaveCount int `json:"saveCount"`
}

// DaemonStatus is a snapshot of the daemon's live state, suitable for
// JSON serialization by the diagnostic HTTP endpoint.
type DaemonStatus struct {
	Uptime      string                    `json:"uptime"`
	Version     string                    `json:"version"`
	SourceID    string                    `json:"sourceId"`
	WSConnected bool                      `json:"wsConnected"`
	Games       map[string]GameStatusInfo `json:"games"`
}

// Status returns a snapshot of the daemon's current state.
// Safe to call from any goroutine (uses RLock).
func (d *Daemon) Status() DaemonStatus {
	d.mu.RLock()
	defer d.mu.RUnlock()

	saveCounts := make(map[string]int, len(d.cfg.Games))
	for _, gameID := range d.watchedDirs {
		saveCounts[gameID]++
	}

	games := make(map[string]GameStatusInfo, len(d.cfg.Games))
	for gameID, cfg := range d.cfg.Games {
		games[gameID] = GameStatusInfo{
			SavePath:       cfg.SavePath,
			Enabled:        cfg.Enabled,
			Watching:       saveCounts[gameID] > 0,
			FileExtensions: cfg.FileExtensions,
			SaveCount:      saveCounts[gameID],
		}
	}

	return DaemonStatus{
		Uptime:      time.Since(d.startTime).Truncate(time.Second).String(),
		Version:     d.cfg.Version,
		SourceID:    d.cfg.SourceID,
		WSConnected: d.ws.IsConnected(),
		Games:       games,
	}
}
