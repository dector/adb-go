package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/dector/adb-go/internal/daemon"
)

const usage = `adb-god is the foreground adb-go daemon process.

Usage:
  adb-god [--socket PATH]

The daemon listens on a Unix domain socket and implements only the minimal
control protocol for ping, status, and shutdown. It does not persist devices,
transports, forwards, sessions, or authentication state yet.

Socket path selection uses --socket when provided, otherwise ADB_GO_DAEMON_SOCKET,
then XDG_RUNTIME_DIR, then a per-user temporary directory.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("adb-god", flag.ContinueOnError)
	fs.SetOutput(stderr)
	socketPath := fs.String("socket", "", "absolute Unix domain socket path")
	fs.Usage = func() { fmt.Fprint(stderr, usage) }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprint(stderr, "adb-god: unexpected arguments\n\n")
		fs.Usage()
		return 2
	}

	server, err := daemon.NewServer(daemon.Options{SocketPath: *socketPath})
	if err != nil {
		fmt.Fprintf(stderr, "adb-god: %v\n", err)
		return 1
	}
	if err := server.Listen(); err != nil {
		if errors.Is(err, daemon.ErrAlreadyRunning) {
			fmt.Fprintf(stderr, "adb-god: %v\n", err)
			return 1
		}
		fmt.Fprintf(stderr, "adb-god: start daemon: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "adb-god listening on %s\n", server.SocketPath())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := server.Serve(ctx); err != nil {
		fmt.Fprintf(stderr, "adb-god: %v\n", err)
		return 1
	}
	return 0
}
