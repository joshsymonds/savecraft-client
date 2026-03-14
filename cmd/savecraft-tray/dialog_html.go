//go:build windows

package main

import (
	_ "embed"
	"encoding/base64"
	"fmt"
	"html/template"
	"strings"
)

var (
	//go:embed fonts/press-start-2p.woff2
	fontPressStart2P []byte

	//go:embed fonts/rajdhani-600.woff2
	fontRajdhani600 []byte

	//go:embed fonts/rajdhani-700.woff2
	fontRajdhani700 []byte

	// Pre-computed base64 font strings and compiled template, initialized once.
	fontB64PressStart2P string
	fontB64Rajdhani600  string
	fontB64Rajdhani700  string
	dialogTmpl          *template.Template
)

func init() {
	fontB64PressStart2P = base64.StdEncoding.EncodeToString(fontPressStart2P)
	fontB64Rajdhani600 = base64.StdEncoding.EncodeToString(fontRajdhani600)
	fontB64Rajdhani700 = base64.StdEncoding.EncodeToString(fontRajdhani700)
	dialogTmpl = template.Must(template.New("dialog").Parse(dialogTemplate))
}

// dialogTemplateData holds the variables injected into the dialog HTML.
type dialogTemplateData struct {
	Code             string
	FontPressStart2P string
	FontRajdhani600  string
	FontRajdhani700  string
}

// renderDialogHTML produces the complete HTML page for the pairing dialog.
func renderDialogHTML(code string) (string, error) {
	data := dialogTemplateData{
		Code:             code,
		FontPressStart2P: fontB64PressStart2P,
		FontRajdhani600:  fontB64Rajdhani600,
		FontRajdhani700:  fontB64Rajdhani700,
	}

	var buf strings.Builder
	if err := dialogTmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute dialog template: %w", err)
	}

	return buf.String(), nil
}

const dialogTemplate = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<style>
  @font-face {
    font-family: 'Press Start 2P';
    src: url('data:font/woff2;base64,{{.FontPressStart2P}}') format('woff2');
    font-weight: 400;
    font-style: normal;
    font-display: block;
  }
  @font-face {
    font-family: 'Rajdhani';
    src: url('data:font/woff2;base64,{{.FontRajdhani600}}') format('woff2');
    font-weight: 600;
    font-style: normal;
    font-display: block;
  }
  @font-face {
    font-family: 'Rajdhani';
    src: url('data:font/woff2;base64,{{.FontRajdhani700}}') format('woff2');
    font-weight: 700;
    font-style: normal;
    font-display: block;
  }

  * { box-sizing: border-box; margin: 0; padding: 0; }

  body {
    width: 420px;
    height: 480px;
    overflow: hidden;
    background: #05071a;
    color: #e8e0d0;
    font-family: 'Rajdhani', sans-serif;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    -webkit-font-smoothing: antialiased;
  }

  .container {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 32px;
    padding: 40px;
    animation: fade-in 0.4s ease-out;
  }

  /* ── Logo ──────────────────────────────── */

  .logo {
    font-family: 'Press Start 2P', monospace;
    font-size: 18px;
    color: #c8a84e;
    letter-spacing: 6px;
    text-align: center;
  }

  .subtitle {
    font-family: 'Rajdhani', sans-serif;
    font-size: 18px;
    font-weight: 600;
    color: #a0a8cc;
    text-align: center;
    margin-top: 8px;
  }

  /* ── Code display ─────────────────────── */

  .code-section {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 10px;
  }

  .code-label {
    font-size: 16px;
    font-weight: 600;
    color: #a0a8cc;
    letter-spacing: 1px;
    text-transform: uppercase;
  }

  .code-value {
    font-family: 'Press Start 2P', monospace;
    font-size: 28px;
    color: #e8c86e;
    letter-spacing: 8px;
    padding: 16px 28px;
    background: rgba(200, 168, 78, 0.08);
    border: 1px solid rgba(200, 168, 78, 0.25);
    border-radius: 6px;
    text-align: center;
  }

  /* ── Button ───────────────────────────── */

  .link-btn {
    font-family: 'Press Start 2P', monospace;
    font-size: 11px;
    color: #05071a;
    letter-spacing: 1px;
    background: linear-gradient(135deg, #c8a84e, #e8c86e);
    border: none;
    border-radius: 4px;
    padding: 14px 32px;
    cursor: pointer;
    transition: all 0.2s;
    text-transform: uppercase;
  }

  .link-btn:hover {
    background: linear-gradient(135deg, #e8c86e, #f0d888);
    transform: translateY(-1px);
    box-shadow: 0 4px 16px rgba(200, 168, 78, 0.3);
  }

  .link-btn:active {
    transform: translateY(0);
  }

  /* ── Waiting state ────────────────────── */

  .waiting-section {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 16px;
  }

  .waiting-text {
    font-size: 18px;
    font-weight: 600;
    color: #a0a8cc;
    animation: pulse-text 2s ease-in-out infinite;
  }

  .spinner {
    display: flex;
    gap: 6px;
    align-items: center;
  }

  .spinner-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: #c8a84e;
    opacity: 0.4;
    animation: dot-pulse 1.2s ease-in-out infinite;
  }

  .spinner-dot:nth-child(2) { animation-delay: 0.2s; }
  .spinner-dot:nth-child(3) { animation-delay: 0.4s; }

  /* ── Close link ───────────────────────── */

  .close-link {
    font-size: 14px;
    color: #a0a8cc;
    text-decoration: none;
    cursor: pointer;
    transition: color 0.15s;
    border: none;
    background: none;
    font-family: 'Rajdhani', sans-serif;
  }

  .close-link:hover {
    color: #e8e0d0;
  }

  /* ── Decorative border ────────────────── */

  .border-glow {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    height: 2px;
    background: linear-gradient(90deg, transparent, #c8a84e, transparent);
  }

  /* ── Animations ───────────────────────── */

  @keyframes fade-in {
    from { opacity: 0; transform: translateY(10px); }
    to   { opacity: 1; transform: translateY(0); }
  }

  @keyframes pulse-text {
    0%, 100% { opacity: 0.5; }
    50%      { opacity: 1; }
  }

  @keyframes dot-pulse {
    0%, 80%, 100% { opacity: 0.4; transform: scale(1); }
    40%           { opacity: 1; transform: scale(1.3); }
  }

  .hidden { display: none; }
</style>
</head>
<body>
  <div class="border-glow"></div>
  <div class="container">
    <div>
      <div class="logo">SAVECRAFT</div>
      <div class="subtitle">Installed successfully</div>
    </div>

    <div class="code-section">
      <span class="code-label">Your link code</span>
      <div class="code-value">{{.Code}}</div>
    </div>

    <button id="link-btn" class="link-btn" onclick="onLinkClick()">Link Account</button>

    <div id="waiting" class="waiting-section hidden">
      <div class="spinner">
        <span class="spinner-dot"></span>
        <span class="spinner-dot"></span>
        <span class="spinner-dot"></span>
      </div>
      <span class="waiting-text">Waiting for pairing...</span>
    </div>

    <button class="close-link" onclick="onCloseClick()">Skip for now</button>
  </div>

  <script>
    function onLinkClick() {
      // Open browser, then close the dialog.
      openLink().then(function() { closeDialog(); });
    }

    function onCloseClick() {
      closeDialog();
    }

    // Called from Go via Eval when pairing completes.
    function onPaired() {
      closeDialog();
    }
  </script>
</body>
</html>`
