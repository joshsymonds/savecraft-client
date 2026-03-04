//go:build windows

package cmd

import (
	_ "embed"
	"fmt"
	"log/slog"
	"runtime"

	webview "github.com/webview/webview_go"

	"github.com/joshsymonds/savecraft.gg/internal/envfile"
	"github.com/joshsymonds/savecraft.gg/internal/pairclient"
)

//go:embed assets/pair.html
var pairDialogHTML string

// showPairDialog opens a branded webview window for entering a pairing code.
// It blocks until the dialog is closed. Returns nil on successful pairing.
func showPairDialog(appName, serverURL string, logger *slog.Logger) error {
	type result struct {
		err error
	}
	ch := make(chan result, 1)

	go func() {
		runtime.LockOSThread()

		w := webview.New(false)
		if w == nil {
			ch <- result{err: fmt.Errorf("failed to create webview")}
			return
		}
		defer w.Destroy()

		w.SetTitle("Savecraft — Pair Device")
		w.SetSize(380, 400, webview.HintFixed)

		envPath := envfile.EnvFilePath(appName)

		if err := w.Bind("pair", func(code string) error {
			logger.Info("pairing", slog.String("code_length", fmt.Sprintf("%d", len(code))))

			res, err := pairclient.ClaimCode(serverURL, code)
			if err != nil {
				logger.Error("pair failed", slog.String("error", err.Error()))
				return fmt.Errorf("pairing failed: %w", err)
			}

			if writeErr := envfile.Write(envPath, map[string]string{
				"SAVECRAFT_AUTH_TOKEN": res.Token,
				"SAVECRAFT_SERVER_URL": res.ServerURL,
			}); writeErr != nil {
				logger.Error("write config", slog.String("error", writeErr.Error()))
				return fmt.Errorf("failed to save credentials: %w", writeErr)
			}

			logger.Info("paired successfully", slog.String("env_path", envPath))
			return nil
		}); err != nil {
			ch <- result{err: fmt.Errorf("bind pair function: %w", err)}
			return
		}

		w.SetHtml(pairDialogHTML)
		w.Run()

		ch <- result{err: nil}
	}()

	r := <-ch
	return r.err
}
