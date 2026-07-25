package adb_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	adb "github.com/dector/adb-go"
)

const containerIntegrationEnv = "ADB_GO_CONTAINER_INTEGRATION"

func TestContainerADBDInstrumentedImplementedFunctionality(t *testing.T) {
	if os.Getenv(containerIntegrationEnv) == "" {
		t.Skipf("set %s=1 to run instrumented integration tests against containerized adbd", containerIntegrationEnv)
	}

	testTimeout := 2 * time.Minute
	if os.Getenv("ADB_GO_CONTAINER_BUILD") != "" {
		testTimeout = 30 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	runtime := containerRuntime(t)
	addr := startContainerADBD(t, ctx, runtime)

	client, err := adb.Connect(ctx, addr)
	if err != nil {
		t.Fatalf("Connect(%q) failed: %v", addr, err)
	}
	defer client.Close()

	t.Run("shell", func(t *testing.T) {
		out, err := client.Shell(ctx, "printf adb-go-container-shell-ok")
		if err != nil {
			t.Fatalf("Shell() failed: %v", err)
		}
		if got := strings.TrimSpace(string(out)); got != "adb-go-container-shell-ok" {
			t.Fatalf("Shell() output = %q, want adb-go-container-shell-ok", got)
		}
	})

	t.Run("shell stream", func(t *testing.T) {
		var out bytes.Buffer
		if err := client.ShellStream(ctx, "printf adb-go-container-stream-ok", &out); err != nil {
			t.Fatalf("ShellStream() failed: %v", err)
		}
		if got := out.String(); got != "adb-go-container-stream-ok" {
			t.Fatalf("ShellStream() output = %q, want adb-go-container-stream-ok", got)
		}
	})

	t.Run("open service", func(t *testing.T) {
		stream, err := client.OpenService(ctx, "shell:printf adb-go-container-service-ok")
		if err != nil {
			t.Fatalf("OpenService() failed: %v", err)
		}
		defer stream.Close()

		var out bytes.Buffer
		if _, err := out.ReadFrom(stream); err != nil {
			t.Fatalf("ReadFrom(service stream) failed: %v", err)
		}
		if got := out.String(); got != "adb-go-container-service-ok" {
			t.Fatalf("OpenService() output = %q, want adb-go-container-service-ok", got)
		}
	})

	t.Run("push and pull", func(t *testing.T) {
		remoteDir := fmt.Sprintf("/tmp/adb-go-container-%d", time.Now().UnixNano())
		remotePath := remoteDir + "/roundtrip.txt"
		defer client.Shell(context.Background(), "rm -rf "+remoteDir)

		if _, err := client.Shell(ctx, "mkdir -p "+remoteDir); err != nil {
			t.Fatalf("create remote temp dir failed: %v", err)
		}

		localDir := t.TempDir()
		localPushPath := filepath.Join(localDir, "push.txt")
		want := []byte("hello from adb-go containerized adbd\n")
		if err := os.WriteFile(localPushPath, want, 0o666); err != nil {
			t.Fatalf("WriteFile(push source) failed: %v", err)
		}

		if err := client.PushFile(ctx, localPushPath, remotePath); err != nil {
			t.Fatalf("PushFile() failed: %v", err)
		}

		localPullPath := filepath.Join(localDir, "pull.txt")
		if err := client.PullFile(ctx, remotePath, localPullPath); err != nil {
			t.Fatalf("PullFile() failed: %v", err)
		}

		got, err := os.ReadFile(localPullPath)
		if err != nil {
			t.Fatalf("ReadFile(pulled file) failed: %v", err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("pulled file = %q, want %q", got, want)
		}
	})
}

func startContainerADBD(t *testing.T, ctx context.Context, runtime string) string {
	t.Helper()

	image := os.Getenv("ADB_GO_CONTAINER_IMAGE")
	if image == "" {
		image = "adb-go-linux-adbd"
	}

	if os.Getenv("ADB_GO_CONTAINER_BUILD") != "" {
		runCommand(t, ctx, runtime, "build", "-f", "docker/linux-adbd/Containerfile", "-t", image, "docker/linux-adbd")
	}

	cid := strings.TrimSpace(runCommand(t, ctx, runtime, "run", "-d", "--rm", "-p", "127.0.0.1::5555", image))
	if cid == "" {
		t.Fatalf("%s run returned an empty container id", runtime)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = exec.CommandContext(cleanupCtx, runtime, "rm", "-f", cid).Run()
	})

	addr := ""
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		out, err := commandOutput(ctx, runtime, "port", cid, "5555/tcp")
		if err == nil {
			addr = strings.TrimSpace(out)
			if addr != "" {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if addr == "" {
		t.Fatalf("%s did not publish adbd port for container %s", runtime, cid)
	}
	if strings.Contains(addr, "\n") {
		addr = strings.Split(addr, "\n")[0]
	}

	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var lastErr error
	for waitCtx.Err() == nil {
		client, err := adb.Connect(waitCtx, addr)
		if err == nil {
			_ = client.Close()
			return addr
		}
		lastErr = err
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("containerized adbd at %s did not become ready: %v", addr, lastErr)
	return ""
}

func containerRuntime(t *testing.T) string {
	t.Helper()

	if runtime := os.Getenv("ADB_GO_CONTAINER_RUNTIME"); runtime != "" {
		if _, err := exec.LookPath(runtime); err != nil {
			t.Fatalf("containerized adbd integration requested, but %s was not found in PATH: %v", runtime, err)
		}
		return runtime
	}
	if _, err := exec.LookPath("podman"); err == nil {
		return "podman"
	}
	if _, err := exec.LookPath("docker"); err == nil {
		return "docker"
	}
	t.Fatal("containerized adbd integration requested, but neither podman nor docker was found in PATH")
	return ""
}

func runCommand(t *testing.T, ctx context.Context, name string, args ...string) string {
	t.Helper()

	out, err := commandOutput(ctx, name, args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func commandOutput(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("%s %s failed: %w\nstdout:\n%s\nstderr:\n%s", name, strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return stdout.String(), nil
}
