package main

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/dector/adb-go/cmd/adb-go/internal/compat"
)

func TestRunCLIDefaultsToCustomMode(t *testing.T) {
	calls := recordingRunners()
	var stdout, stderr bytes.Buffer

	code := runCLI([]string{"version"}, &stdout, &stderr, "adb-go", "", calls.custom, calls.compat)

	if code != 10 {
		t.Fatalf("runCLI exit code = %d, want custom runner code 10", code)
	}
	if calls.customCalls != 1 || calls.compatCalls != 0 {
		t.Fatalf("custom calls = %d, compat calls = %d; want custom only", calls.customCalls, calls.compatCalls)
	}
	if got := stdout.String(); !strings.Contains(got, "custom:[version]") {
		t.Fatalf("stdout = %q, want custom runner output", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunCLIUsesCustomModeFromEnv(t *testing.T) {
	calls := recordingRunners()
	var stdout, stderr bytes.Buffer

	code := runCLI([]string{"help"}, &stdout, &stderr, "adb", "custom", calls.custom, calls.compat)

	if code != 10 {
		t.Fatalf("runCLI exit code = %d, want custom runner code 10", code)
	}
	if calls.customCalls != 1 || calls.compatCalls != 0 {
		t.Fatalf("custom calls = %d, compat calls = %d; want custom env override", calls.customCalls, calls.compatCalls)
	}
}

func TestRunCLIUsesCompatModeFromEnv(t *testing.T) {
	calls := recordingRunners()
	var stdout, stderr bytes.Buffer

	code := runCLI([]string{"version"}, &stdout, &stderr, "adb-go", "compat", calls.custom, calls.compat)

	if code != 20 {
		t.Fatalf("runCLI exit code = %d, want compat runner code 20", code)
	}
	if calls.customCalls != 0 || calls.compatCalls != 1 {
		t.Fatalf("custom calls = %d, compat calls = %d; want compat only", calls.customCalls, calls.compatCalls)
	}
	if got := stdout.String(); !strings.Contains(got, "compat:[version]") {
		t.Fatalf("stdout = %q, want compat runner output", got)
	}
}

func TestRunCLIRejectsInvalidEnvMode(t *testing.T) {
	calls := recordingRunners()
	var stdout, stderr bytes.Buffer

	code := runCLI([]string{"version"}, &stdout, &stderr, "adb-go", "invalid", calls.custom, calls.compat)

	if code != 2 {
		t.Fatalf("runCLI exit code = %d, want 2", code)
	}
	if calls.customCalls != 0 || calls.compatCalls != 0 {
		t.Fatalf("custom calls = %d, compat calls = %d; want no runner calls", calls.customCalls, calls.compatCalls)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "invalid ADB_GO_MODE") || !strings.Contains(got, `"custom"`) || !strings.Contains(got, `"compat"`) {
		t.Fatalf("stderr = %q, want invalid mode guidance", got)
	}
}

func TestRunCLIUsesCompatModeForADBExecutableBasenames(t *testing.T) {
	for _, executablePath := range []string{"adb", "/usr/local/bin/adb", "adb.exe", `/opt/android/adb.exe`} {
		t.Run(executablePath, func(t *testing.T) {
			calls := recordingRunners()
			var stdout, stderr bytes.Buffer

			code := runCLI([]string{"devices"}, &stdout, &stderr, executablePath, "", calls.custom, calls.compat)

			if code != 20 {
				t.Fatalf("runCLI exit code = %d, want compat runner code 20", code)
			}
			if calls.customCalls != 0 || calls.compatCalls != 1 {
				t.Fatalf("custom calls = %d, compat calls = %d; want compat only", calls.customCalls, calls.compatCalls)
			}
		})
	}
}

func TestCompatPlaceholderReportsNotImplemented(t *testing.T) {
	var stdout, stderr bytes.Buffer
	calls := recordingRunners()

	code := runCLI([]string{"devices"}, &stdout, &stderr, "adb", "", calls.custom, compat.Run)

	if code != 1 {
		t.Fatalf("runCLI compat placeholder exit code = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "compat mode is not implemented yet") {
		t.Fatalf("stderr = %q, want compat placeholder message", got)
	}
}

type runnerCalls struct {
	customCalls int
	compatCalls int
}

func recordingRunners() *runnerCalls { return &runnerCalls{} }

func (r *runnerCalls) custom(args []string, stdout, stderr io.Writer) int {
	r.customCalls++
	fmt.Fprintf(stdout, "custom:%v\n", args)
	return 10
}

func (r *runnerCalls) compat(args []string, stdout, stderr io.Writer) int {
	r.compatCalls++
	fmt.Fprintf(stdout, "compat:%v\n", args)
	return 20
}
