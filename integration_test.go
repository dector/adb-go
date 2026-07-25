package adb_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	adb "github.com/dector/adb-go"
)

func TestIntegrationConnectAndShell(t *testing.T) {
	addr := os.Getenv("ADB_GO_INTEGRATION_ADDR")
	if addr == "" {
		t.Skip("set ADB_GO_INTEGRATION_ADDR to run integration tests against a real ADB TCP device or emulator")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := adb.Connect(ctx, addr)
	if err != nil {
		t.Fatalf("Connect(%q) failed: %v", addr, err)
	}
	defer client.Close()

	out, err := client.Shell(ctx, "echo adb-go-integration-ok")
	if err != nil {
		t.Fatalf("Shell failed: %v", err)
	}

	got := strings.TrimSpace(strings.ReplaceAll(string(out), "\r\n", "\n"))
	if got != "adb-go-integration-ok" {
		t.Fatalf("Shell output = %q, want %q", got, "adb-go-integration-ok")
	}
}
