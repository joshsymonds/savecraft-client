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
}

// Registry provides access to the plugin manifest and downloads.
type Registry interface {
	FetchManifest(ctx context.Context) (map[string]PluginInfo, error)
	Download(ctx context.Context, url string) ([]byte, error)
}
