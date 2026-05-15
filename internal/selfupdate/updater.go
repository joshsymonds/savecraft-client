// Package selfupdate checks for and applies daemon binary updates.
package selfupdate

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joshsymonds/savecraft.gg/internal/daemon"
	"github.com/joshsymonds/savecraft.gg/internal/manifest"
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
	// manifestPubKey verifies the detached manifest signature. It is always the
	// embedded release key in production and is never disableable (R3); tests in
	// this package override it with a generated key.
	manifestPubKey ed25519.PublicKey
	cacheDir       string
	client         *http.Client
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
	Tray      map[string]daemon.UpdateInfo `json:"tray"`
}

// New creates an HTTPUpdater that checks installURL for updates.
func New(installURL string, pubKey ed25519.PublicKey, cacheDir string, opts ...Option) *HTTPUpdater {
	updater := &HTTPUpdater{
		installURL:     installURL,
		pubKey:         pubKey,
		manifestPubKey: signing.PublicKey(),
		cacheDir:       cacheDir,
		client:         &http.Client{Timeout: defaultUpdateTimeout},
	}
	for _, opt := range opts {
		opt(updater)
	}
	return updater
}

// Check fetches the daemon manifest and returns update info if a newer version is available.
func (u *HTTPUpdater) Check(ctx context.Context, currentVersion, platform string) (*daemon.CheckResult, error) {
	manifestBytes, err := u.fetchBytes(ctx, u.installURL+"/daemon/manifest.json")
	if err != nil {
		return nil, fmt.Errorf("fetch manifest: %w", err)
	}
	sigBytes, err := u.fetchBytes(ctx, u.installURL+"/daemon/manifest.json.sig")
	if err != nil {
		// A missing/unreachable signature must never downgrade to "skip
		// verification" — it is a hard failure.
		return nil, fmt.Errorf("fetch manifest signature: %w", err)
	}

	// Verify the detached signature over the literal manifest bytes BEFORE
	// reading any field (R2: verify-then-parse, never the reverse).
	parsed, err := manifest.VerifyAndParse[manifestResponse](u.manifestPubKey, manifestBytes, sigBytes)
	if err != nil {
		return nil, fmt.Errorf("verify manifest: %w", err)
	}

	daemonInfo, ok := parsed.Platforms[platform]
	if !ok {
		return nil, ErrNoPlatform
	}

	if !isNewer(parsed.Version, currentVersion) {
		return nil, ErrUpToDate
	}

	daemonInfo.Version = parsed.Version

	result := &daemon.CheckResult{
		Daemon: &daemonInfo,
	}

	// Include tray info if available for this platform.
	if trayInfo, trayOK := parsed.Tray[platform]; trayOK {
		trayInfo.Version = parsed.Version
		result.Tray = &trayInfo
	}

	return result, nil
}

// fetchBytes GETs url and returns the full response body, erroring on any
// non-200 status. Used for the manifest and its detached signature.
func (u *HTTPUpdater) fetchBytes(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	resp, err := u.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned %d", rawURL, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", rawURL, err)
	}
	return body, nil
}

// validateUpdateOrigin enforces that rawURL is an https URL whose host exactly
// matches the host of the build-time-pinned install origin (u.installURL). The
// pin is derived locally and never from the manifest or the server-pushed
// SourceUpdateAvailable message (R6, finding 4.1). An empty or unparseable
// install origin fails closed: no update is trusted.
func (u *HTTPUpdater) validateUpdateOrigin(rawURL string) error {
	pinned, err := url.Parse(u.installURL)
	if err != nil || pinned.Scheme != "https" || pinned.Host == "" {
		return fmt.Errorf("refusing update: no trustworthy pinned install origin (%q)", u.installURL)
	}
	got, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("parse update URL %q: %w", rawURL, err)
	}
	if got.Scheme != "https" {
		return fmt.Errorf("refusing update: URL scheme %q is not https (%q)", got.Scheme, rawURL)
	}
	if got.Host != pinned.Host {
		return fmt.Errorf("refusing update: host %q is not the pinned install origin %q", got.Host, pinned.Host)
	}
	return nil
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
	// Pin both URLs to the locally-trusted install origin BEFORE any network
	// access. This is the chokepoint for the server-pushed SourceUpdateAvailable
	// path as well as the manifest path (finding 4.1, R6).
	if err := u.validateUpdateOrigin(info.URL); err != nil {
		return err
	}
	if err := u.validateUpdateOrigin(info.SignatureURL); err != nil {
		return err
	}

	if err := os.MkdirAll(u.cacheDir, 0o750); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}

	tempBinaryPath := filepath.Join(u.cacheDir, "daemon-update.tmp")
	tempSigPath := filepath.Join(u.cacheDir, "daemon-update.tmp.sig")

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

	// Unconditional: a nil/invalid key makes signing.Verify fail closed.
	// The self-update signature can never be skipped (epic R3).
	if verifyErr := signing.Verify(u.pubKey, binaryBytes, sigBytes); verifyErr != nil {
		return fmt.Errorf("signature verification: %w", verifyErr)
	}

	actualHash := sha256.Sum256(binaryBytes)
	actualHex := hex.EncodeToString(actualHash[:])
	if actualHex != info.SHA256 {
		return fmt.Errorf("sha256 mismatch: got %s, want %s", actualHex, info.SHA256)
	}

	replaceErr := replaceBinary(tempBinaryPath, binaryPath)
	if replaceErr != nil {
		return fmt.Errorf("replace binary: %w", replaceErr)
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
	closed := false
	defer func() {
		if !closed {
			_ = outFile.Close()
		}
	}()

	if _, copyErr := io.Copy(outFile, resp.Body); copyErr != nil {
		return fmt.Errorf("write %s: %w", destPath, copyErr)
	}

	if syncErr := outFile.Sync(); syncErr != nil {
		return fmt.Errorf("sync %s: %w", destPath, syncErr)
	}

	closed = true
	if closeErr := outFile.Close(); closeErr != nil {
		return fmt.Errorf("close %s: %w", destPath, closeErr)
	}

	return nil
}
