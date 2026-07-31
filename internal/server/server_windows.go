//go:build windows

package server

import (
	"context"
	"errors"
	"fmt"
)

var ErrAlreadyRunning = errors.New("adb-gos server already running")

type Options struct {
	SocketPath string
}

type Server struct{}

func NewServer(opts Options) (*Server, error) {
	return nil, fmt.Errorf("adb-gos Unix domain socket is not supported on windows")
}
func (s *Server) SocketPath() string { return "" }
func (s *Server) Listen() error {
	return fmt.Errorf("adb-gos Unix domain socket is not supported on windows")
}
func (s *Server) Serve(ctx context.Context) error {
	return fmt.Errorf("adb-gos Unix domain socket is not supported on windows")
}
func (s *Server) Done() <-chan struct{} { ch := make(chan struct{}); close(ch); return ch }
func (s *Server) Shutdown() error       { return nil }
