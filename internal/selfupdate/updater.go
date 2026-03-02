// Package selfupdate checks for and applies daemon binary updates.
package selfupdate

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joshsymonds/savecraft.gg/internal/daemon"
	"github.com/joshsymonds/savecraft.gg/internal/signing"
)

const defaultUpdateTimeout = 120 * time.Second

// ErrNoPlatform indicates the manifest has no update for the requested platform.
var ErrNoPlatform = errors.New("no update available for platform")

// ErrUpToDate indicates the current version is already at or ahead of the manifest version.
var ErrUpToDate = errors.New("already up to date")

// HTTPUpdater checks a remote manifest for daemon updates and applies them.
// Downloads are unauthenticated — the install worker serves public binaries.
type HTTPUpdater struct {
	installURL string
	pubKey     ed25519.PublicKey
	cacheDir   string
	client     *http.Client
}

// Option configures an HTTPUpdater.
type Option func(*HTTPUpdater)

// WithHTTPClient overrides the default HTTP client used for update requests.
func WithHTTPClient(c *http.Client) Option {
	return func(u *HTTPUpdater) { u.client = c }
}

type manifestResponse struct {
	Version   string                       `json:"version"`
	Platforms map[string]daemon.UpdateInfo `json:"platforms"`
}

// New creates an HTTPUpdater that checks installURL for updates.
func New(installURL string, pubKey ed25519.PublicKey, cacheDir string, opts ...Option) *HTTPUpdater {
	updater := &HTTPUpdater{
		installURL: installURL,
		pubKey:     pubKey,
		cacheDir:   cacheDir,
		client:     &http.Client{Timeout: defaultUpdateTimeout},
	}
	for _, opt := range opts {
		opt(updater)
	}
	return updater
}

// Check fetches the daemon manifest and returns update info if a newer version is available.
func (u *HTTPUpdater) Check(ctx context.Context, currentVersion, platform string) (*daemon.UpdateInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.installURL+"/daemon/manifest.json", nil)
	if err != nil {
		return nil, fmt.Errorf("create manifest request: %w", err)
	}

	resp, err := u.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manifest request returned %d", resp.StatusCode)
	}

	var manifest manifestResponse
	decodeErr := json.NewDecoder(resp.Body).Decode(&manifest)
	if decodeErr != nil {
		return nil, fmt.Errorf("decode manifest: %w", decodeErr)
	}

	info, ok := manifest.Platforms[platform]
	if !ok {
		return nil, ErrNoPlatform
	}

	if !isNewer(manifest.Version, currentVersion) {
		return nil, ErrUpToDate
	}

	info.Version = manifest.Version
	return &info, nil
}

// isNewer returns true if latest is a strictly newer semver than current.
func isNewer(latest, current string) bool {
	parse := func(v string) []int {
		parts := make([]int, 0, 3)
		for s := range strings.SplitSeq(v, ".") {
			n, atoiErr := strconv.Atoi(s)
			if atoiErr != nil {
				n = 0
			}
			parts = append(parts, n)
		}
		return parts
	}
	latestParts, currentParts := parse(latest), parse(current)
	for i := 0; i < len(latestParts) || i < len(currentParts); i++ {
		lp, cp := 0, 0
		if i < len(latestParts) {
			lp = latestParts[i]
		}
		if i < len(currentParts) {
			cp = currentParts[i]
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
	if err := os.MkdirAll(u.cacheDir, 0o750); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}

	tempBinaryPath := filepath.Join(u.cacheDir, "savecraft-daemon.new")
	tempSigPath := filepath.Join(u.cacheDir, "savecraft-daemon.new.sig")

	defer cleanupTempFiles(tempBinaryPath, tempSigPath)

	if err := downloadToFile(ctx, info.URL, tempBinaryPath, u.client); err != nil {
		return fmt.Errorf("download binary: %w", err)
	}

	if err := downloadToFile(ctx, info.SignatureURL, tempSigPath, u.client); err != nil {
		return fmt.Errorf("download signature: %w", err)
	}

	binaryBytes, err := os.ReadFile(filepath.Clean(tempBinaryPath))
	if err != nil {
		return fmt.Errorf("read downloaded binary: %w", err)
	}

	sigBytes, sigReadErr := os.ReadFile(filepath.Clean(tempSigPath))
	if sigReadErr != nil {
		return fmt.Errorf("read downloaded signature: %w", sigReadErr)
	}

	if u.pubKey != nil {
		verifyErr := signing.Verify(u.pubKey, binaryBytes, sigBytes)
		if verifyErr != nil {
			return fmt.Errorf("signature verification: %w", verifyErr)
		}
	}

	actualHash := sha256.Sum256(binaryBytes)
	actualHex := hex.EncodeToString(actualHash[:])
	if actualHex != info.SHA256 {
		return fmt.Errorf("sha256 mismatch: got %s, want %s", actualHex, info.SHA256)
	}

	renameErr := os.Rename(tempBinaryPath, binaryPath)
	if renameErr != nil {
		return fmt.Errorf("replace binary: %w", renameErr)
	}

	chmodErr := os.Chmod(binaryPath, 0o700)
	if chmodErr != nil {
		return fmt.Errorf("chmod binary: %w", chmodErr)
	}

	return nil
}

// cleanupTempFiles removes temporary download files, ignoring errors since
// these are best-effort cleanup of files in a temp/cache directory.
func cleanupTempFiles(paths ...string) {
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			// Best-effort cleanup; nothing actionable on failure.
			continue
		}
	}
}

func downloadToFile(ctx context.Context, url, destPath string, client *http.Client) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s returned %d", url, resp.StatusCode)
	}

	outFile, err := os.Create(filepath.Clean(destPath))
	if err != nil {
		return fmt.Errorf("create %s: %w", destPath, err)
	}
	defer outFile.Close()

	_, copyErr := io.Copy(outFile, resp.Body)
	if copyErr != nil {
		return fmt.Errorf("write %s: %w", destPath, copyErr)
	}

	return nil
}
