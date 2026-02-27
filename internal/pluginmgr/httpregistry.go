package pluginmgr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	manifestTimeout = 30 * time.Second
	downloadTimeout = 5 * time.Minute
)

// HTTPRegistry fetches plugin metadata and binaries from the savecraft server.
type HTTPRegistry struct {
	serverURL string
	authToken string
	manifest  *http.Client
	download  *http.Client
}

// NewHTTPRegistry creates an HTTPRegistry targeting the given server.
func NewHTTPRegistry(serverURL, authToken string) *HTTPRegistry {
	return &HTTPRegistry{
		serverURL: serverURL,
		authToken: authToken,
		manifest:  &http.Client{Timeout: manifestTimeout},
		download:  &http.Client{Timeout: downloadTimeout},
	}
}

// FetchManifest retrieves the plugin manifest from the server.
func (r *HTTPRegistry) FetchManifest(
	ctx context.Context,
) (map[string]PluginInfo, error) {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet,
		r.serverURL+"/api/v1/plugins/manifest", nil,
	)
	if err != nil {
		return nil, fmt.Errorf("create manifest request: %w", err)
	}
	if r.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+r.authToken)
	}

	resp, err := r.manifest.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"manifest returned status %d", resp.StatusCode,
		)
	}

	var body struct {
		Plugins map[string]PluginInfo `json:"plugins"`
	}
	if decodeErr := json.NewDecoder(resp.Body).Decode(&body); decodeErr != nil {
		return nil, fmt.Errorf("decode manifest: %w", decodeErr)
	}
	return body.Plugins, nil
}

// Download fetches raw bytes from the given URL.
func (r *HTTPRegistry) Download(
	ctx context.Context, url string,
) ([]byte, error) {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet, url, nil,
	)
	if err != nil {
		return nil, fmt.Errorf("create download request: %w", err)
	}
	if r.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+r.authToken)
	}

	resp, err := r.download.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download plugin: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"download returned status %d", resp.StatusCode,
		)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read download body: %w", err)
	}
	return data, nil
}
