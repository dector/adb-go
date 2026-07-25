package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	adb "github.com/dector/adb-go"
)

const usage = `adb-go is a pure-Go Android Debug Bridge client.

Usage:
  adb-go <command> [arguments]

Commands:
  help        Show this help message
  shell       Run a shell command on a connected device
  push        Push one local file to a connected device
  pull        Pull one remote file from a connected device

adb-go is not a full replacement for the official adb binary yet. The CLI is a
thin wrapper around the adb-go library and will grow command coverage gradually.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

type connectionOptions struct {
	addr *string
}

func addConnectionFlags(fs *flag.FlagSet) connectionOptions {
	return connectionOptions{
		addr: fs.String("addr", "", "explicit TCP device address, for example 127.0.0.1:5555"),
	}
}

func (o connectionOptions) address(fs *flag.FlagSet) (string, bool) {
	if flagWasProvided(fs, "addr") {
		addr := strings.TrimSpace(*o.addr)
		return addr, addr != ""
	}

	addr := strings.TrimSpace(os.Getenv("ADB_GO_ADDR"))
	return addr, addr != ""
}

func flagWasProvided(fs *flag.FlagSet, name string) bool {
	provided := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			provided = true
		}
	})
	return provided
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		return 0
	}

	switch args[0] {
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	case "shell":
		return runShell(args[1:], stdout, stderr)
	case "push":
		return runPush(args[1:], stdout, stderr)
	case "pull":
		return runPull(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "adb-go: unknown command %q\n\n", args[0])
		fmt.Fprint(stderr, usage)
		return 2
	}
}

const shellUsage = `Usage:
  adb-go shell --addr HOST[:PORT] COMMAND [ARG...]

Runs one shell command on the explicitly addressed TCP ADB device. The address
comes from --addr, or from ADB_GO_ADDR when --addr is omitted. COMMAND and all
following arguments are joined with spaces and sent as one shell command string,
for example:

  adb-go shell --addr 127.0.0.1:5555 echo hello
`

func runShell(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("shell", flag.ContinueOnError)
	fs.SetOutput(stderr)
	conn := addConnectionFlags(fs)
	fs.Usage = func() { fmt.Fprint(stderr, shellUsage) }

	if err := fs.Parse(args); err != nil {
		return 2
	}
	addr, ok := conn.address(fs)
	if !ok {
		fmt.Fprint(stderr, "adb-go shell: missing required --addr or ADB_GO_ADDR\n\n")
		fs.Usage()
		return 2
	}
	if fs.NArg() == 0 {
		fmt.Fprint(stderr, "adb-go shell: missing shell command\n\n")
		fs.Usage()
		return 2
	}

	cmd := strings.Join(fs.Args(), " ")
	client, err := adb.Connect(context.Background(), addr)
	if err != nil {
		fmt.Fprintf(stderr, "adb-go shell: connect to %s: %v\n", addr, err)
		return 1
	}
	defer client.Close()

	if err := client.ShellStream(context.Background(), cmd, stdout); err != nil {
		fmt.Fprintf(stderr, "adb-go shell: %v\n", err)
		return 1
	}
	return 0
}

const pushUsage = `Usage:
  adb-go push --addr HOST[:PORT] LOCAL_PATH REMOTE_PATH

Pushes exactly one local file to the explicitly addressed TCP ADB device. The
address comes from --addr, or from ADB_GO_ADDR when --addr is omitted. For
example:

  adb-go push --addr 127.0.0.1:5555 ./local.txt /data/local/tmp/local.txt
`

func runPush(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("push", flag.ContinueOnError)
	fs.SetOutput(stderr)
	conn := addConnectionFlags(fs)
	fs.Usage = func() { fmt.Fprint(stderr, pushUsage) }

	if err := fs.Parse(args); err != nil {
		return 2
	}
	addr, ok := conn.address(fs)
	if !ok {
		fmt.Fprint(stderr, "adb-go push: missing required --addr or ADB_GO_ADDR\n\n")
		fs.Usage()
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprint(stderr, "adb-go push: requires exactly LOCAL_PATH and REMOTE_PATH\n\n")
		fs.Usage()
		return 2
	}

	localPath, remotePath := fs.Arg(0), fs.Arg(1)
	client, err := adb.Connect(context.Background(), addr)
	if err != nil {
		fmt.Fprintf(stderr, "adb-go push: connect to %s: %v\n", addr, err)
		return 1
	}
	defer client.Close()

	if err := client.PushFile(context.Background(), localPath, remotePath); err != nil {
		fmt.Fprintf(stderr, "adb-go push: %v\n", err)
		return 1
	}
	return 0
}

const pullUsage = `Usage:
  adb-go pull --addr HOST[:PORT] [--overwrite] REMOTE_PATH LOCAL_PATH

Pulls exactly one remote file from the explicitly addressed TCP ADB device. The
address comes from --addr, or from ADB_GO_ADDR when --addr is omitted. By default,
the command refuses to replace an existing local destination; pass --overwrite to
replace it deliberately. For example:

  adb-go pull --addr 127.0.0.1:5555 /data/local/tmp/remote.txt ./remote.txt
`

func runPull(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pull", flag.ContinueOnError)
	fs.SetOutput(stderr)
	conn := addConnectionFlags(fs)
	overwrite := fs.Bool("overwrite", false, "replace an existing local destination")
	fs.Usage = func() { fmt.Fprint(stderr, pullUsage) }

	if err := fs.Parse(args); err != nil {
		return 2
	}
	addr, ok := conn.address(fs)
	if !ok {
		fmt.Fprint(stderr, "adb-go pull: missing required --addr or ADB_GO_ADDR\n\n")
		fs.Usage()
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprint(stderr, "adb-go pull: requires exactly REMOTE_PATH and LOCAL_PATH\n\n")
		fs.Usage()
		return 2
	}

	remotePath, localPath := fs.Arg(0), fs.Arg(1)
	client, err := adb.Connect(context.Background(), addr)
	if err != nil {
		fmt.Fprintf(stderr, "adb-go pull: connect to %s: %v\n", addr, err)
		return 1
	}
	defer client.Close()

	if *overwrite {
		err = client.PullFileWithOptions(context.Background(), remotePath, localPath, adb.PullOptions{Overwrite: true})
	} else {
		err = client.PullFile(context.Background(), remotePath, localPath)
	}
	if err != nil {
		fmt.Fprintf(stderr, "adb-go pull: %v\n", err)
		return 1
	}
	return 0
}
