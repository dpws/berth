//go:build windows

package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// notifyTimeout bounds the toast. PowerShell is not quick to start and the
// agent would rather drop a notification than hold a request open.
const notifyTimeout = 15 * time.Second

// toastAppID is the application the toast is raised as.
//
// Windows will not show a toast from an application it does not know, and
// registering berth-clipd as one means writing to the start menu and the
// registry - a lot of ceremony for a line of text. This is PowerShell's own
// identifier, which is on every Windows machine already, so the toast arrives
// attributed to it. That is the whole of the trick.
const toastAppID = `{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe`

// toastScript raises the toast. The text comes in through the environment
// rather than the script, so a session named with a quote cannot end the
// string it is being interpolated into and start being PowerShell instead.
const toastScript = `
$ErrorActionPreference = 'Stop'
[void][Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime]
[void][Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom, ContentType = WindowsRuntime]
$template = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent(
    [Windows.UI.Notifications.ToastTemplateType]::ToastText02)
$lines = $template.GetElementsByTagName('text')
[void]$lines[0].AppendChild($template.CreateTextNode($env:BERTH_NOTIFY_TITLE))
[void]$lines[1].AppendChild($template.CreateTextNode($env:BERTH_NOTIFY_BODY))
$toast = [Windows.UI.Notifications.ToastNotification]::new($template)
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier($env:BERTH_NOTIFY_APPID).Show($toast)
`

// raiseNotification shows a Windows toast.
func raiseNotification(title, body string) error {
	ctx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive",
		"-ExecutionPolicy", "Bypass", "-Command", toastScript)
	cmd.Env = append(cmd.Environ(),
		"BERTH_NOTIFY_TITLE="+title,
		"BERTH_NOTIFY_BODY="+body,
		"BERTH_NOTIFY_APPID="+toastAppID)
	hideWindow(cmd)

	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return fmt.Errorf("powershell timed out after %s", notifyTimeout)
	}
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return fmt.Errorf("powershell: %s", msg)
		}
		return err
	}
	return nil
}
