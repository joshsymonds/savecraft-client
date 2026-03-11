package cmd

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	"github.com/joshsymonds/savecraft.gg/internal/daemon"
	"github.com/joshsymonds/savecraft.gg/internal/envfile"
	"github.com/joshsymonds/savecraft.gg/internal/osfs"
	"github.com/joshsymonds/savecraft.gg/internal/pluginmgr"
	pb "github.com/joshsymonds/savecraft.gg/internal/proto/savecraft/v1"
	"github.com/joshsymonds/savecraft.gg/internal/runner"
	"github.com/joshsymonds/savecraft.gg/internal/selfupdate"
	"github.com/joshsymonds/savecraft.gg/internal/signing"
	"github.com/joshsymonds/savecraft.gg/internal/watcher"
	"github.com/joshsymonds/savecraft.gg/internal/wsconn"
)

type subsystems struct {
	fsys    *osfs.OSFS
	watcher *watcher.FSWatcher
	runner  *runner.WazeroRunner
	plugins *pluginmgr.Manager
	updater *selfupdate.HTTPUpdater
	ws      *wsconn.Client
}

func (s *subsystems) close(ctx context.Context, logger *slog.Logger) {
	if closeErr := s.runner.Close(ctx); closeErr != nil {
		logger.ErrorContext(ctx, "close runner", slog.String("error", closeErr.Error()))
	}

	if closeErr := s.watcher.Close(); closeErr != nil {
		logger.ErrorContext(ctx, "close watcher", slog.String("error", closeErr.Error()))
	}
}

func createSubsystems(ctx context.Context, cfg *appConfig, appName string, logger *slog.Logger) (*subsystems, error) {
	fsys := osfs.New()

	wt, err := watcher.New()
	if err != nil {
		return nil, fmt.Errorf("create watcher: %w", err)
	}

	skipVerify := os.Getenv("SAVECRAFT_SKIP_VERIFY") != ""

	var opts []runner.Option
	if skipVerify {
		logger.WarnContext(ctx, "plugin signature verification disabled via SAVECRAFT_SKIP_VERIFY")
	} else {
		opts = append(opts, runner.WithVerifier(signing.PublicKey()))
	}

	wr, err := runner.NewWazeroRunner(ctx, opts...)
	if err != nil {
		wt.Close()

		return nil, fmt.Errorf("create runner: %w", err)
	}

	var pubKey ed25519.PublicKey
	if !skipVerify {
		pubKey = signing.PublicKey()
	}

	cacheDir := pluginmgr.DefaultCacheDir(appName)
	cache := pluginmgr.NewCache(cacheDir)
	reg := pluginmgr.NewHTTPRegistry(cfg.ServerURL, cfg.AuthToken)

	mgr := pluginmgr.NewManager(reg, cache, wr, pubKey, logger)
	if cfg.PluginDir != "" {
		mgr.SetLocalDir(cfg.PluginDir)
	}

	updateCacheDir := filepath.Join(cacheDir, "updates")
	updater := selfupdate.New(cfg.InstallURL, pubKey, updateCacheDir)

	wsURL := cfg.ServerURL + "/ws/daemon"
	ws := wsconn.New(wsURL, cfg.AuthToken, wsconn.WithLogger(logger))

	return &subsystems{
		fsys:    fsys,
		watcher: wt,
		runner:  wr,
		plugins: mgr,
		updater: updater,
		ws:      ws,
	}, nil
}

// registerResult holds credentials returned by WS registration.
type registerResult struct {
	SourceUUID        string
	Token             string
	LinkCode          string
	LinkCodeExpiresAt string
}

// appConfig holds bootstrap configuration loaded from the environment.
type appConfig struct {
	ServerURL  string
	InstallURL string
	AuthToken  string `json:"-"`
	PluginDir  string
	Daemon     daemon.Config
}

func loadConfig(serverURLDefault, installURLDefault string) (*appConfig, error) {
	serverURL := os.Getenv("SAVECRAFT_SERVER_URL")
	if serverURL == "" {
		serverURL = serverURLDefault
	}
	if serverURL == "" {
		return nil, fmt.Errorf("SAVECRAFT_SERVER_URL is required")
	}

	installURL := os.Getenv("SAVECRAFT_INSTALL_URL")
	if installURL == "" {
		installURL = installURLDefault
	}
	if installURL == "" {
		return nil, fmt.Errorf("SAVECRAFT_INSTALL_URL is required")
	}

	authToken := os.Getenv("SAVECRAFT_AUTH_TOKEN")

	sourceID := os.Getenv("SAVECRAFT_SOURCE_ID")
	if sourceID == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return nil, fmt.Errorf("get hostname for source ID: %w", err)
		}

		sourceID = hostname
	}

	pluginDir := os.Getenv("SAVECRAFT_PLUGIN_DIR")

	cfgVersion := os.Getenv("SAVECRAFT_VERSION")
	if cfgVersion == "" {
		cfgVersion = "dev"
	}

	sourceUUID := os.Getenv("SAVECRAFT_SOURCE_UUID")

	return &appConfig{
		ServerURL:  serverURL,
		InstallURL: installURL,
		AuthToken:  authToken,
		PluginDir:  pluginDir,
		Daemon: daemon.Config{
			ServerURL:  serverURL,
			AuthToken:  authToken,
			SourceID:   sourceID,
			SourceUUID: sourceUUID,
			Version:    cfgVersion,
			Games:      make(map[string]daemon.GameConfig),
		},
	}, nil
}

// daemonConfigDefaults creates a daemon.Config with minimal defaults.
func daemonConfigDefaults(sourceID, version string) daemon.Config {
	return daemon.Config{
		SourceID: sourceID,
		Version:  version,
		Games:    make(map[string]daemon.GameConfig),
	}
}

// autoRegister checks if the daemon has credentials. If not, it connects to
// /ws/register and sends a Register proto to create a new source. Persists
// credentials to the env file and updates config in place. Returns
// (result, true, nil) if registration happened, or (nil, false, nil) if
// credentials already exist.
func autoRegister(ctx context.Context, cfg *appConfig, envPath string) (*registerResult, bool, error) {
	if cfg.AuthToken != "" {
		return nil, false, nil
	}

	result, err := wsRegister(ctx, cfg.ServerURL, cfg.Daemon.SourceID)
	if err != nil {
		return nil, false, fmt.Errorf("source registration: %w", err)
	}

	// Persist credentials.
	if writeErr := envfile.Write(envPath, map[string]string{
		"SAVECRAFT_AUTH_TOKEN":  result.Token,
		"SAVECRAFT_SOURCE_UUID": result.SourceUUID,
		"SAVECRAFT_SERVER_URL":  cfg.ServerURL,
	}); writeErr != nil {
		return nil, false, fmt.Errorf("persist credentials: %w", writeErr)
	}

	// Update config in place.
	cfg.AuthToken = result.Token
	cfg.Daemon.AuthToken = result.Token
	cfg.Daemon.SourceUUID = result.SourceUUID

	return result, true, nil
}

// wsRegister connects to /ws/register and performs source registration over
// WebSocket + protobuf.
func wsRegister(ctx context.Context, serverURL, hostname string) (*registerResult, error) {
	wsURL := strings.Replace(serverURL, "http://", "ws://", 1)
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	wsURL += "/ws/register"

	conn, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{},
	})
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", wsURL, err)
	}
	defer conn.CloseNow()

	// Send Register message.
	registerMsg := &pb.Message{Payload: &pb.Message_Register{Register: &pb.Register{
		Hostname: hostname,
		Os:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		Device:   daemon.DetectDevice(),
	}}}
	data, marshalErr := proto.Marshal(registerMsg)
	if marshalErr != nil {
		return nil, fmt.Errorf("marshal register: %w", marshalErr)
	}
	if writeErr := conn.Write(ctx, websocket.MessageBinary, data); writeErr != nil {
		return nil, fmt.Errorf("send register: %w", writeErr)
	}

	// Read RegisterResult response.
	_, respData, readErr := conn.Read(ctx)
	if readErr != nil {
		return nil, fmt.Errorf("read register result: %w", readErr)
	}

	var respMsg pb.Message
	if unmarshalErr := proto.Unmarshal(respData, &respMsg); unmarshalErr != nil {
		return nil, fmt.Errorf("unmarshal register result: %w", unmarshalErr)
	}

	rr, ok := respMsg.Payload.(*pb.Message_RegisterResult)
	if !ok {
		return nil, fmt.Errorf("unexpected response type: %T", respMsg.Payload)
	}

	result := rr.RegisterResult
	expiresAt := ""
	if result.LinkCodeExpiresAt != nil {
		expiresAt = result.LinkCodeExpiresAt.AsTime().Format("2006-01-02T15:04:05Z")
	}

	conn.Close(websocket.StatusNormalClosure, "registered")

	return &registerResult{
		SourceUUID:        result.SourceUuid,
		Token:             result.SourceToken,
		LinkCode:          result.LinkCode,
		LinkCodeExpiresAt: expiresAt,
	}, nil
}

// loadEnvFileDefaults reads the env file and sets environment variables
// for any keys not already set. Auto-registration writes credentials here
// on first boot; subsequent runs pick them up automatically.
func loadEnvFileDefaults(appName string) {
	loadEnvFileDefaultsFromPath(envfile.EnvFilePath(appName))
}

func loadEnvFileDefaultsFromPath(path string) {
	vars, err := envfile.Read(path)
	if err != nil {
		return
	}

	for key, value := range vars {
		if os.Getenv(key) == "" {
			os.Setenv(key, value)
		}
	}
}
