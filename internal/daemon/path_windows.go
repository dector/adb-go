//go:build windows

package daemon

import "fmt"

const EnvSocket = "ADB_GO_DAEMON_SOCKET"

func DefaultSocketPath() (string, error) {
	return "", fmt.Errorf("adb-god Unix domain socket is not supported on windows")
}
