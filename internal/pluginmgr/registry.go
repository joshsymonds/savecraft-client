// Package pluginmgr handles plugin download, verification, caching, and loading.
package pluginmgr

import "context"

// PluginInfo describes a plugin available from the registry.
type PluginInfo struct {
	GameID  string `json:"gameId"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
	URL     string `json:"url"`
}

// Registry provides access to the plugin manifest and downloads.
type Registry interface {
	FetchManifest(ctx context.Context) (map[string]PluginInfo, error)
	Download(ctx context.Context, url string) ([]byte, error)
}
