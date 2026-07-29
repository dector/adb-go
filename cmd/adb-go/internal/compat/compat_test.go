package compat

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunHelpForms(t *testing.T) {
	for _, args := range [][]string{nil, []string{"help"}, []string{"--help"}, []string{"-h"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			stdout, stderr, code := runForTest(args...)

			if code != 0 {
				t.Fatalf("exit code = %d, want 0", code)
			}
			if stderr != "" {
				t.Fatalf("stderr = %q, want empty", stderr)
			}
			assertContains(t, stdout, "Android Debug Bridge version 1.0.41 (adb-go compat)")
			assertContains(t, stdout, "Usage: adb [global options] command [command options]")
			assertContains(t, stdout, " -H HOST    adb server host name [default=localhost]")
			assertContains(t, stdout, " help       show this help message")
			assertContains(t, stdout, " version    show version num")
			assertNotContains(t, stdout, "targets")
			assertNotContains(t, stdout, "install-apk")
			assertNotContains(t, stdout, "ADB_GO_ADDR")
		})
	}
}

func TestRunVersion(t *testing.T) {
	oldVersion := Version
	Version = "test-version"
	defer func() { Version = oldVersion }()

	stdout, stderr, code := runForTest("version")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	assertContains(t, stdout, "Android Debug Bridge version 1.0.41 (adb-go compat)\n")
	assertContains(t, stdout, "Version test-version\n")
	assertContains(t, stdout, "Installed as adb-go compat mode\n")
	assertContains(t, stdout, "Server target localhost:5037\n")
}

func TestRunVersionAcceptsHostAndPortGlobalOptions(t *testing.T) {
	stdout, stderr, code := runForTest("-H", "192.0.2.10", "-P", "7777", "version")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	assertContains(t, stdout, "Server target 192.0.2.10:7777\n")
}

func TestRunVersionAcceptsJoinedHostAndPortGlobalOptions(t *testing.T) {
	stdout, stderr, code := runForTest("-Hlocalhost", "-P5038", "version")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	assertContains(t, stdout, "Server target localhost:5038\n")
}

func TestRunUnknownCommand(t *testing.T) {
	stdout, stderr, code := runForTest("devices")

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if stderr != "adb: unknown command devices\n" {
		t.Fatalf("stderr = %q, want adb-shaped unknown command", stderr)
	}
}

func TestRunRejectsCustomOnlyFlags(t *testing.T) {
	for _, flag := range []string{"--addr", "--usb"} {
		t.Run(flag, func(t *testing.T) {
			stdout, stderr, code := runForTest(flag, "127.0.0.1:5555", "version")

			if code != 1 {
				t.Fatalf("exit code = %d, want 1", code)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			assertContains(t, stderr, "adb: unknown option "+flag)
		})
	}
}

func TestRunGlobalHostOptionRequiresValue(t *testing.T) {
	stdout, stderr, code := runForTest("-H")

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if stderr != "adb: option -H requires an argument\n" {
		t.Fatalf("stderr = %q, want missing-argument error", stderr)
	}
}

func runForTest(args ...string) (stdout string, stderr string, code int) {
	var out, err bytes.Buffer
	code = Run(args, &out, &err)
	return out.String(), err.String(), code
}

func assertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("got %q, want to contain %q", got, want)
	}
}

func assertNotContains(t *testing.T, got, unwanted string) {
	t.Helper()
	if strings.Contains(got, unwanted) {
		t.Fatalf("got %q, want not to contain %q", got, unwanted)
	}
}
