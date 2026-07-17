package pluginmgr

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/joshsymonds/savecraft-client/internal/manifest"
)

const (
	manifestTimeout = 30 * time.Second
	downloadTimeout = 5 * time.Minute
)

// HTTPRegistry fetches the signed plugin manifest and plugin binaries from the
// server origin (the Worker that also serves plugin binaries — all plugin
// delivery lives on one origin; task #18). The manifest is a CI-signed
// aggregate published to R2 and served verbatim; the daemon verifies it
// against the embedded release key before trusting any field, and pins every
// URL to the server origin (no trust in the Worker — findings 4.3 / R12,
// plugin half of R2).
type HTTPRegistry struct {
	serverURL string
	pubKey    ed25519.PublicKey
	manifest  *http.Client
	download  *http.Client
}

// Option configures an HTTPRegistry.
type Option func(*HTTPRegistry)

// WithHTTPClient overrides both HTTP clients (used by tests to trust an
// httptest TLS server).
func WithHTTPClient(c *http.Client) Option {
	return func(reg *HTTPRegistry) {
		reg.manifest = c
		reg.download = c
	}
}

// NewHTTPRegistry creates an HTTPRegistry pinned to serverURL. pubKey must be
// the embedded release key (signing.PublicKey()); it is the unconditional
// trust anchor for the signed aggregate manifest.
func NewHTTPRegistry(serverURL string, pubKey ed25519.PublicKey, opts ...Option) *HTTPRegistry {
	reg := &HTTPRegistry{
		serverURL: serverURL,
		pubKey:    pubKey,
		manifest:  &http.Client{Timeout: manifestTimeout},
		download:  &http.Client{Timeout: downloadTimeout},
	}
	for _, opt := range opts {
		opt(reg)
	}
	return reg
}

// FetchManifest retrieves the signed aggregate plugin manifest from the
// server origin, verifies the detached signature over the literal bytes
// against the embedded key, and only then parses it. A missing/unreachable
// signature is a hard error, never a downgrade to "skip verification".
func (reg *HTTPRegistry) FetchManifest(
	ctx context.Context,
) (map[string]PluginInfo, error) {
	manifestURL := reg.serverURL + "/plugins/manifest.json"
	sigURL := manifestURL + ".sig"

	if err := manifest.RequirePinnedHTTPS(manifestURL, reg.serverURL); err != nil {
		return nil, fmt.Errorf("plugin manifest origin: %w", err)
	}

	body, err := reg.get(ctx, reg.manifest, manifestURL)
	if err != nil {
		return nil, fmt.Errorf("fetch plugin manifest: %w", err)
	}
	sig, err := reg.get(ctx, reg.manifest, sigURL)
	if err != nil {
		return nil, fmt.Errorf("fetch plugin manifest signature: %w", err)
	}

	parsed, err := manifest.VerifyAndParse[struct {
		Plugins map[string]PluginInfo `json:"plugins"`
	}](reg.pubKey, body, sig)
	if err != nil {
		return nil, fmt.Errorf("verify plugin manifest: %w", err)
	}
	return parsed.Plugins, nil
}

// Download fetches raw bytes from url, which must be an https URL on the
// pinned server origin (the signed manifest only ever carries such URLs;
// this is defense-in-depth, consistent with the self-update path).
func (reg *HTTPRegistry) Download(
	ctx context.Context, url string,
) ([]byte, error) {
	if err := manifest.RequirePinnedHTTPS(url, reg.serverURL); err != nil {
		return nil, fmt.Errorf("plugin download origin: %w", err)
	}
	return reg.get(ctx, reg.download, url)
}

func (reg *HTTPRegistry) get(
	ctx context.Context, client *http.Client, rawURL string,
) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned status %d", rawURL, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", rawURL, err)
	}
	return data, nil
}
