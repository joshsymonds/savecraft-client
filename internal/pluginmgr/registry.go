// Package pluginmgr handles plugin download, verification, caching, and loading.
package pluginmgr

import "context"

// PluginInfo describes a plugin available from the registry.
//
//nolint:tagliatelle // manifest JSON uses snake_case to match server wire format
type PluginInfo struct {
	GameID         string            `json:"game_id"`
	Name           string            `json:"name"`
	Version        string            `json:"version"`
	SHA256         string            `json:"sha256"`
	URL            string            `json:"url"`
	DefaultPaths   map[string]string `json:"default_paths"`
	FileExtensions []string          `json:"file_extensions"`
	FilePatterns   []string          `json:"file_patterns,omitempty"`
	ExcludeDirs    []string          `json:"exclude_dirs,omitempty"`
	// Unit selects the save-unit granularity: "" (or "file", the default)
	// means each matching file is its own save; "directory" means each
	// resolved default_paths directory is one save, whose Members are
	// tar-archived together and dispatched as a single unit.
	Unit string `json:"unit,omitempty"`
	// Members lists include patterns, relative to the save directory, that
	// select which files belong to a directory-unit save. Ignored for
	// file-unit plugins.
	Members []string `json:"members,omitempty"`
	// MinDaemonVersion is the minimum daemon version (dotted numeric, compared
	// via internal/version.IsNewer) required to load this plugin. A daemon
	// older than this requirement skips the plugin entirely rather than risk
	// loading a plugin whose contract it doesn't understand yet.
	MinDaemonVersion string `json:"min_daemon_version,omitempty"`
}

// Registry provides access to the plugin manifest and downloads.
type Registry interface {
	FetchManifest(ctx context.Context) (map[string]PluginInfo, error)
	Download(ctx context.Context, url string) ([]byte, error)
}
