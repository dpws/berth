//go:build !windows

package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const notifyTimeout = 15 * time.Second

// raiseNotification shows a desktop notification the way the machine does.
//
// The agent exists for Windows, where the terminal has no notification escape
// sequence berth can use. Elsewhere it is mostly a way to test the endpoint,
// and notify-send is what every freedesktop session already has.
func raiseNotification(title, body string) error {
	if _, err := exec.LookPath("notify-send"); err != nil {
		return errors.New("no notify-send on this machine")
	}
	ctx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "notify-send", "--app-name=berth", title, body)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return fmt.Errorf("notify-send timed out after %s", notifyTimeout)
	}
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return fmt.Errorf("notify-send: %s", msg)
		}
		return err
	}
	return nil
}
