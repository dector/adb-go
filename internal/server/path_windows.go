//go:build windows

package server

import "fmt"

const EnvSocket = "ADB_GO_SERVER_SOCKET"

func DefaultSocketPath() (string, error) {
	return "", fmt.Errorf("adb-gos Unix domain socket is not supported on windows")
}
