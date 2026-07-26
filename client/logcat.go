package client

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// LogcatOptions controls Client.Logcat behavior.
type LogcatOptions struct {
	// Dump requests logcat's dump-and-exit mode by passing -d. Without Dump,
	// logcat follows the device log stream until the device closes it or the
	// context is canceled.
	Dump bool
}

// Logcat streams Android logcat output from the connected device into stdout.
// adb-go intentionally exposes only a small, explicit subset of logcat behavior
// for now; callers that need unsupported flags can use ShellStream directly.
func (c *Client) Logcat(ctx context.Context, stdout io.Writer, opts LogcatOptions) error {
	if stdout == nil {
		return fmt.Errorf("adb logcat stdout writer is nil")
	}
	cmd := logcatCommand(opts)
	if err := c.ShellStream(ctx, cmd, stdout); err != nil {
		return fmt.Errorf("adb logcat: %w", err)
	}
	return nil
}

func logcatCommand(opts LogcatOptions) string {
	args := []string{"logcat"}
	if opts.Dump {
		args = append(args, "-d")
	}
	return strings.Join(args, " ")
}
