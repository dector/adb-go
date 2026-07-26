package client

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const installRemoteDir = "/data/local/tmp"

var (
	installTempCounter    uint64
	makeInstallRemotePath = defaultInstallRemotePath
)

// InstallOptions controls InstallAPKWithOptions behavior.
type InstallOptions struct {
	// Replace allows the package manager to replace an already-installed app by
	// passing -r to pm install. adb-go intentionally supports only this small,
	// explicit subset of install behavior for now instead of mirroring every
	// official adb install flag.
	Replace bool
}

// InstallAPK installs one local APK on the connected device. It pushes the APK
// to a generated file below /data/local/tmp, runs pm install on that temporary
// file through shell:, and then best-effort removes the temporary file.
func (c *Client) InstallAPK(ctx context.Context, localPath string) error {
	return c.InstallAPKWithOptions(ctx, localPath, InstallOptions{})
}

// InstallAPKWithOptions installs one local APK on the connected device using a
// deliberately small adb-go API. The package manager output is returned in the
// error when installation fails so callers can display the device-provided
// reason directly.
func (c *Client) InstallAPKWithOptions(ctx context.Context, localPath string, opts InstallOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if localPath == "" {
		return fmt.Errorf("adb install apk local path is empty")
	}

	remotePath := makeInstallRemotePath(localPath)
	if err := c.PushFile(ctx, localPath, remotePath); err != nil {
		return fmt.Errorf("adb install apk %q: push temporary APK: %w", localPath, err)
	}

	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = c.Shell(cleanupCtx, "rm -f -- "+shellQuote(remotePath))
	}()

	args := []string{"pm", "install"}
	if opts.Replace {
		args = append(args, "-r")
	}
	args = append(args, shellQuote(remotePath))

	var out strings.Builder
	err := c.ShellStream(ctx, strings.Join(args, " "), &out)
	output := out.String()
	if err != nil {
		if strings.TrimSpace(output) != "" {
			return fmt.Errorf("adb install apk %q: run package manager: %w; output: %s", localPath, err, formatPMOutput(output))
		}
		return fmt.Errorf("adb install apk %q: run package manager: %w", localPath, err)
	}
	if !pmInstallSucceeded(output) {
		return fmt.Errorf("adb install apk %q: package manager failed: %s", localPath, formatPMOutput(output))
	}
	return nil
}

func defaultInstallRemotePath(localPath string) string {
	base := sanitizeInstallBase(filepath.Base(localPath))
	counter := atomic.AddUint64(&installTempCounter, 1)
	stamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	return installRemoteDir + "/adb-go-install-" + stamp + "-" + strconv.Itoa(os.Getpid()) + "-" + strconv.FormatUint(counter, 36) + "-" + base
}

func sanitizeInstallBase(base string) string {
	base = strings.TrimSpace(base)
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "app.apk"
	}
	var b strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "app.apk"
	}
	return b.String()
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func pmInstallSucceeded(output string) bool {
	return strings.HasPrefix(strings.TrimSpace(output), "Success")
}

func formatPMOutput(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return "package manager returned no success marker and no output"
	}
	return output
}
