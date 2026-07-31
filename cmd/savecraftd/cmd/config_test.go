package cmd

import (
	"context"
	"log/slog"
	"testing"

	"github.com/joshsymonds/savecraft-client/internal/pluginmgr"
)

// fakeRegistry is a minimal pluginmgr.Registry that serves a fixed manifest,
// used to observe how newPluginManager wires the daemon version into the
// min_daemon_version guard without touching the real HTTP/signature path.
type fakeRegistry struct {
	manifest map[string]pluginmgr.PluginInfo
}

func (r *fakeRegistry) FetchManifest(context.Context) (map[string]pluginmgr.PluginInfo, error) {
	return r.manifest, nil
}

func (r *fakeRegistry) Download(context.Context, string) ([]byte, error) {
	return nil, nil
}

type fakeLoader struct{}

func (fakeLoader) LoadPlugin(context.Context, string, []byte, []byte) error {
	return nil
}

func TestNewPluginManager_StampedVersionFiltersGatedPlugin(t *testing.T) {
	reg := &fakeRegistry{manifest: map[string]pluginmgr.PluginInfo{
		"gated":   {GameID: "gated", Version: "1.0.0", MinDaemonVersion: "2.0.0"},
		"ungated": {GameID: "ungated", Version: "1.0.0"},
	}}
	cache := pluginmgr.NewCache(t.TempDir())
	logger := slog.New(slog.DiscardHandler)

	mgr := newPluginManager(reg, cache, fakeLoader{}, nil, "1.5.0", logger)

	manifest, err := mgr.Manifests(context.Background())
	if err != nil {
		t.Fatalf("Manifests: %v", err)
	}
	if _, ok := manifest["gated"]; ok {
		t.Errorf("expected gated plugin (min_daemon_version 2.0.0) to be filtered for daemon 1.5.0, got %+v", manifest)
	}
	if _, ok := manifest["ungated"]; !ok {
		t.Errorf("expected ungated plugin to remain, got %+v", manifest)
	}
}

func TestNewPluginManager_DevVersionDoesNotFilter(t *testing.T) {
	reg := &fakeRegistry{manifest: map[string]pluginmgr.PluginInfo{
		"gated": {GameID: "gated", Version: "1.0.0", MinDaemonVersion: "2.0.0"},
	}}
	cache := pluginmgr.NewCache(t.TempDir())
	logger := slog.New(slog.DiscardHandler)

	mgr := newPluginManager(reg, cache, fakeLoader{}, nil, "dev", logger)

	manifest, err := mgr.Manifests(context.Background())
	if err != nil {
		t.Fatalf("Manifests: %v", err)
	}
	if _, ok := manifest["gated"]; !ok {
		t.Errorf(
			"expected gated plugin to remain when daemon version is unstamped \"dev\" (no-op guard), got %+v",
			manifest,
		)
	}
}
