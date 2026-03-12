//go:build windows

package main

import (
	"fmt"
	"html"
	"os/exec"
)

// showToast displays a Windows toast notification using PowerShell.
// On click, Windows opens the launch URI in the default browser.
func showToast(title, body, clickURL string) error {
	// Use PowerShell with the Windows.UI.Notifications API.
	// The launch attribute makes the toast open the URL when clicked.
	// Escape values for safe XML interpolation.
	script := fmt.Sprintf(`
[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
[Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom, ContentType = WindowsRuntime] | Out-Null

$xml = @"
<toast launch="%s" activationType="protocol" scenario="reminder">
  <visual>
    <binding template="ToastGeneric">
      <text>%s</text>
      <text>%s</text>
    </binding>
  </visual>
</toast>
"@

$doc = New-Object Windows.Data.Xml.Dom.XmlDocument
$doc.LoadXml($xml)
$toast = [Windows.UI.Notifications.ToastNotification]::new($doc)
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier("Savecraft").Show($toast)
`, html.EscapeString(clickURL), html.EscapeString(title), html.EscapeString(body))

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("powershell toast: %w", err)
	}

	go func() { _ = cmd.Wait() }()

	return nil
}
