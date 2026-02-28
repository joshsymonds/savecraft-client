// Package selfupdate checks for and applies daemon binary updates.
package selfupdate

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joshsymonds/savecraft.gg/internal/daemon"
	"github.com/joshsymonds/savecraft.gg/internal/signing"
)

// HTTPUpdater checks a remote manifest for daemon updates and applies them.
type HTTPUpdater struct {
	serverURL string
	authToken string
	pubKey    ed25519.PublicKey
	cacheDir  string
	client    *http.Client
}

type manifestResponse struct {
	Version   string                      `json:"version"`
	Platforms map[string]daemon.UpdateInfo `json:"platforms"`
}

// New creates an HTTPUpdater that checks serverURL for updates.
func New(serverURL, authToken string, pubKey ed25519.PublicKey, cacheDir string) *HTTPUpdater {
	return &HTTPUpdater{
		serverURL: serverURL,
		authToken: authToken,
		pubKey:    pubKey,
		cacheDir:  cacheDir,
		client:    http.DefaultClient,
	}
}

// Check fetches the daemon manifest and returns update info if a newer version is available.
func (u *HTTPUpdater) Check(ctx context.Context, currentVersion, platform string) (*daemon.UpdateInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.serverURL+"/api/v1/daemon/manifest", nil)
	if err != nil {
		return nil, fmt.Errorf("create manifest request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+u.authToken)

	resp, err := u.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manifest request returned %d", resp.StatusCode)
	}

	var manifest manifestResponse
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}

	info, ok := manifest.Platforms[platform]
	if !ok {
		return nil, nil
	}

	if !isNewer(manifest.Version, currentVersion) {
		return nil, nil
	}

	info.Version = manifest.Version
	return &info, nil
}

// isNewer returns true if latest is a strictly newer semver than current.
func isNewer(latest, current string) bool {
	parse := func(v string) []int {
		var parts []int
		for s := range strings.SplitSeq(v, ".") {
			n, _ := strconv.Atoi(s)
			parts = append(parts, n)
		}
		return parts
	}
	l, c := parse(latest), parse(current)
	for i := 0; i < len(l) || i < len(c); i++ {
		lp, cp := 0, 0
		if i < len(l) {
			lp = l[i]
		}
		if i < len(c) {
			cp = c[i]
		}
		if lp > cp {
			return true
		}
		if lp < cp {
			return false
		}
	}
	return false
}

// Apply downloads a new daemon binary, verifies its signature and checksum, and replaces binaryPath.
func (u *HTTPUpdater) Apply(ctx context.Context, info *daemon.UpdateInfo, binaryPath string) error {
	if err := os.MkdirAll(u.cacheDir, 0o755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}

	tempBinaryPath := filepath.Join(u.cacheDir, "savecraft-daemon.new")
	tempSigPath := filepath.Join(u.cacheDir, "savecraft-daemon.new.sig")

	defer func() {
		os.Remove(tempBinaryPath)
		os.Remove(tempSigPath)
	}()

	if err := downloadToFile(ctx, info.URL, tempBinaryPath, u.authToken, u.client); err != nil {
		return fmt.Errorf("download binary: %w", err)
	}

	if err := downloadToFile(ctx, info.SignatureURL, tempSigPath, u.authToken, u.client); err != nil {
		return fmt.Errorf("download signature: %w", err)
	}

	binaryBytes, err := os.ReadFile(tempBinaryPath)
	if err != nil {
		return fmt.Errorf("read downloaded binary: %w", err)
	}

	sigBytes, err := os.ReadFile(tempSigPath)
	if err != nil {
		return fmt.Errorf("read downloaded signature: %w", err)
	}

	if u.pubKey != nil {
		if err := signing.Verify(u.pubKey, binaryBytes, sigBytes); err != nil {
			return fmt.Errorf("signature verification: %w", err)
		}
	}

	actualHash := sha256.Sum256(binaryBytes)
	actualHex := hex.EncodeToString(actualHash[:])
	if actualHex != info.SHA256 {
		return fmt.Errorf("sha256 mismatch: got %s, want %s", actualHex, info.SHA256)
	}

	if err := os.Rename(tempBinaryPath, binaryPath); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}

	if err := os.Chmod(binaryPath, 0o755); err != nil {
		return fmt.Errorf("chmod binary: %w", err)
	}

	return nil
}

func downloadToFile(ctx context.Context, url, destPath, authToken string, client *http.Client) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+authToken)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s returned %d", url, resp.StatusCode)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create %s: %w", destPath, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("write %s: %w", destPath, err)
	}

	return nil
}

