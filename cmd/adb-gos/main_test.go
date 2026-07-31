//go:build !windows

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunRejectsUnexpectedArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"extra"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("run(extra) exit code = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "unexpected arguments") || !strings.Contains(got, "Usage:") {
		t.Fatalf("stderr = %q, want argument error and usage", got)
	}
}

func TestRunRejectsRelativeSocketPath(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--socket", "relative.sock"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run(relative socket) exit code = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "socket path must be absolute") {
		t.Fatalf("stderr = %q, want absolute path error", got)
	}
}

func TestRunRefusesExistingRegularFileSocketPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "adb-gos.sock")
	if err := os.WriteFile(path, []byte("not a socket"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"--socket", path}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run(existing file socket) exit code = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "exists and is not a Unix socket") {
		t.Fatalf("stderr = %q, want file refusal", got)
	}
}
