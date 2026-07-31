//go:build !windows

package server

import (
	"fmt"
	"os"
	"path/filepath"
)

const EnvSocket = "ADB_GO_SERVER_SOCKET"

func DefaultSocketPath() (string, error) {
	if override := os.Getenv(EnvSocket); override != "" {
		if !filepath.IsAbs(override) {
			return "", fmt.Errorf("%s must be an absolute path", EnvSocket)
		}
		return override, nil
	}
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" && filepath.IsAbs(runtimeDir) {
		return filepath.Join(runtimeDir, "adb-go", "adb-gos.sock"), nil
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("adb-go-%d", os.Getuid()), "adb-gos.sock"), nil
}
