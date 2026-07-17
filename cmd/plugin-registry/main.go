// Command plugin-registry generates client-manifest.json for a plugin from its plugin.toml.
//
// Usage:
//
//	plugin-registry [--version <version>] <plugin-dir>  # writes <plugin-dir>/client-manifest.json
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"slices"

	"github.com/BurntSushi/toml"
)

//nolint:tagliatelle // manifest JSON uses snake_case to match plugin.toml field names
type pluginTOML struct {
	GameID         string   `toml:"game_id"         json:"game_id"`
	Sources        []string `toml:"sources"         json:"sources"`
	Icon           string   `toml:"icon"            json:"icon"`
	Name           string   `toml:"name"            json:"name"`
	Description    string   `toml:"description"     json:"description"`
	Channel        string   `toml:"channel"         json:"channel"`
	Coverage       string   `toml:"coverage"        json:"coverage"`
	FileExtensions []string `toml:"file_extensions" json:"file_extensions"`
	FilePatterns   []string `toml:"file_patterns"   json:"file_patterns,omitempty"`
	ExcludeDirs    []string `toml:"exclude_dirs"    json:"exclude_dirs,omitempty"`
	Homepage       string   `toml:"homepage"        json:"homepage"`
	WorkshopURL    string   `toml:"workshop_url"    json:"workshop_url,omitempty"`
	Limitations    []string `toml:"limitations"     json:"limitations"`

	Attribution  attributionInfo `toml:"attribution"   json:"attribution,omitzero"`
	Author       authorInfo      `toml:"author"        json:"author"`
	DefaultPaths defaultPaths    `toml:"default_paths" json:"default_paths"`
	Reference    referenceTOML   `toml:"reference"     json:"-"`
	AdapterTOML  adapterTOML     `toml:"adapter"       json:"-"`
}

type attributionInfo struct {
	Sources []string `toml:"sources" json:"sources"`
}

type adapterTOML struct {
	AuthProvider string   `toml:"auth_provider"`
	AuthFlow     string   `toml:"auth_flow"`
	Scopes       []string `toml:"scopes"`
	Regions      []string `toml:"regions"`
}

type authorInfo struct {
	Name   string `toml:"name"   json:"name"`
	GitHub string `toml:"github" json:"github"`
}

type defaultPaths struct {
	Windows string `toml:"windows" json:"windows"`
	Linux   string `toml:"linux"   json:"linux"`
	Darwin  string `toml:"darwin"  json:"darwin"`
}

//nolint:tagliatelle // manifest JSON uses snake_case to match plugin.toml field names
type referenceModule struct {
	Name            string            `toml:"name"             json:"name"`
	Description     string            `toml:"description"      json:"description"`
	Parameters      map[string]any    `toml:"-"                json:"parameters,omitempty"`
	SectionMappings map[string]string `toml:"section_mappings" json:"section_mappings,omitempty"`
}

type referenceTOML struct {
	Modules map[string]referenceModule `toml:"modules" json:"-"`
}

type pluginManifest struct {
	pluginTOML
	Version string `json:"version"`
	SHA256  string `json:"sha256,omitempty"`
	URL     string `json:"url,omitempty"`
}

func main() {
	args := os.Args[1:]

	// Parse --version flag (must appear before positional args).
	var versionOverride string
	for i, arg := range args {
		if arg == "--version" && i+1 < len(args) {
			versionOverride = args[i+1]
			args = append(args[:i], args[i+2:]...)
			break
		}
	}

	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: plugin-registry [--version <version>] <plugin-dir>")
		os.Exit(1)
	}

	if err := runSingle(args[0], versionOverride); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runSingle(pluginDir string, versionOverride string) error {
	manifest, err := buildManifest(pluginDir)
	if err != nil {
		return err
	}

	if versionOverride != "" {
		manifest.Version = versionOverride
	}

	outPath := filepath.Join(pluginDir, "client-manifest.json")
	return writeJSON(outPath, manifest)
}

func buildManifest(pluginDir string) (pluginManifest, error) {
	tomlPath := filepath.Join(pluginDir, "plugin.toml")
	var cfg pluginTOML
	if _, err := toml.DecodeFile(tomlPath, &cfg); err != nil {
		return pluginManifest{}, fmt.Errorf("decode %s: %w", tomlPath, err)
	}

	manifest := pluginManifest{
		pluginTOML: cfg,
	}

	// API plugins have no parser WASM.
	if slices.Contains(cfg.Sources, "api") {
		return manifest, nil
	}

	// Mod-only plugins have no parser WASM.
	if slices.Contains(cfg.Sources, "mod") && !slices.Contains(cfg.Sources, "wasm") {
		return manifest, nil
	}

	// Native-only plugins have no parser WASM — just metadata.
	wasmPath := filepath.Join(pluginDir, "parser.wasm")
	if _, err := os.Stat(wasmPath); errors.Is(err, os.ErrNotExist) {
		return manifest, nil
	}

	// WASM plugin: hash parser.wasm.
	hash, err := fileSHA256(wasmPath)
	if err != nil {
		return pluginManifest{}, fmt.Errorf("hash %s: %w", wasmPath, err)
	}

	manifest.SHA256 = hash
	manifest.URL = fmt.Sprintf("plugins/%s/parser.wasm", cfg.GameID)

	return manifest, nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open: %w", err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("read: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	if !strings.HasSuffix(string(data), "\n") {
		data = append(data, '\n')
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
