//go:build windows

package daemon

import (
	"context"
	"errors"
	"fmt"
)

var ErrAlreadyRunning = errors.New("adb-god daemon already running")

type Options struct {
	SocketPath string
}

type Server struct{}

func NewServer(opts Options) (*Server, error) {
	return nil, fmt.Errorf("adb-god Unix domain socket is not supported on windows")
}
func (s *Server) SocketPath() string { return "" }
func (s *Server) Listen() error {
	return fmt.Errorf("adb-god Unix domain socket is not supported on windows")
}
func (s *Server) Serve(ctx context.Context) error {
	return fmt.Errorf("adb-god Unix domain socket is not supported on windows")
}
func (s *Server) Done() <-chan struct{} { ch := make(chan struct{}); close(ch); return ch }
func (s *Server) Shutdown() error       { return nil }
