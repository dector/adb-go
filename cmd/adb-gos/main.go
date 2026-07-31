package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/dector/adb-go/internal/server"
)

const usage = `adb-gos is the foreground adb-go server process.

Usage:
  adb-gos [--socket PATH]

The server listens on a Unix domain socket and implements only the minimal
control protocol for ping, status, and shutdown. It does not persist devices,
transports, forwards, sessions, or authentication state yet.

Socket path selection uses --socket when provided, otherwise ADB_GO_SERVER_SOCKET,
then XDG_RUNTIME_DIR, then a per-user temporary directory.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("adb-gos", flag.ContinueOnError)
	fs.SetOutput(stderr)
	socketPath := fs.String("socket", "", "absolute Unix domain socket path")
	fs.Usage = func() { fmt.Fprint(stderr, usage) }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprint(stderr, "adb-gos: unexpected arguments\n\n")
		fs.Usage()
		return 2
	}

	srv, err := server.NewServer(server.Options{SocketPath: *socketPath})
	if err != nil {
		fmt.Fprintf(stderr, "adb-gos: %v\n", err)
		return 1
	}
	if err := srv.Listen(); err != nil {
		if errors.Is(err, server.ErrAlreadyRunning) {
			fmt.Fprintf(stderr, "adb-gos: %v\n", err)
			return 1
		}
		fmt.Fprintf(stderr, "adb-gos: start server: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "adb-gos listening on %s\n", srv.SocketPath())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := srv.Serve(ctx); err != nil {
		fmt.Fprintf(stderr, "adb-gos: %v\n", err)
		return 1
	}
	return 0
}
