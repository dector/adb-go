package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/dector/adb-go/internal/fakeadb"
	"github.com/dector/adb-go/protocol"
)

func TestRunShowsUsageWithNoArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run(nil, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(nil) exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "Usage:") {
		t.Fatalf("stdout = %q, want usage text", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunShowsUsageForHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"help"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(help) exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "Commands:") {
		t.Fatalf("stdout = %q, want command list", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"devices"}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("run(unknown) exit code = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, `unknown command "devices"`) || !strings.Contains(got, "Usage:") {
		t.Fatalf("stderr = %q, want unknown command error and usage", got)
	}
}

func TestRunShellMissingAddr(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"shell", "echo", "hello"}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("run(shell missing addr) exit code = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "missing required --addr") || !strings.Contains(got, "adb-go shell --addr") {
		t.Fatalf("stderr = %q, want missing addr usage error", got)
	}
}

func TestRunShellMissingCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"shell", "--addr", "127.0.0.1:5555"}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("run(shell missing command) exit code = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "missing shell command") || !strings.Contains(got, "adb-go shell --addr") {
		t.Fatalf("stderr = %q, want missing command usage error", got)
	}
}

func TestRunShellStreamsOutput(t *testing.T) {
	server := fakeadb.Start(t)
	server.Handle("shell:echo hello", writeCLIShellOutput(t, "hello\n"))
	var stdout, stderr bytes.Buffer

	code := run([]string{"shell", "--addr", server.Addr(), "echo", "hello"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(shell) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if stdout.String() != "hello\n" {
		t.Fatalf("stdout = %q, want hello newline", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunShellJoinsCommandArguments(t *testing.T) {
	server := fakeadb.Start(t)
	opened := make(chan string, 1)
	wantService := "shell:pm list packages | grep example"
	server.Handle(wantService, func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		opened <- strings.TrimSuffix(string(open.Payload), "\x00")
		writeCLIShellOutput(t, "package:example\n")(ctx, conn, open)
	})
	var stdout, stderr bytes.Buffer

	code := run([]string{"shell", "--addr", server.Addr(), "pm", "list", "packages", "|", "grep", "example"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(shell) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if got := <-opened; got != wantService {
		t.Fatalf("opened service = %q, want %q", got, wantService)
	}
	if stdout.String() != "package:example\n" {
		t.Fatalf("stdout = %q, want package output", stdout.String())
	}
}

func TestRunShellConnectFailure(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"shell", "--addr", "127.0.0.1:1", "echo", "hello"}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("run(shell connect failure) exit code = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "connect to 127.0.0.1:1") {
		t.Fatalf("stderr = %q, want connect error", got)
	}
}

func writeCLIShellOutput(t testing.TB, output string) fakeadb.ServiceHandler {
	t.Helper()
	return func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		remoteID := uint32(42)
		_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandOKAY, Arg0: remoteID, Arg1: open.Arg0})
		_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandWRTE, Arg0: remoteID, Arg1: open.Arg0, Payload: []byte(output)})
		_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandCLSE, Arg0: remoteID, Arg1: open.Arg0})
	}
}
