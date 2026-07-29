package main

import (
	"fmt"
	"io"
	"path/filepath"
)

type cliMode int

const (
	cliModeCustom cliMode = iota
	cliModeCompat
)

type cliRunner func(args []string, stdout, stderr io.Writer) int

func runCLI(args []string, stdout, stderr io.Writer, executablePath, envMode string, customRun, compatRun cliRunner) int {
	mode, err := selectCLIMode(executablePath, envMode)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	switch mode {
	case cliModeCompat:
		return compatRun(args, stdout, stderr)
	default:
		return customRun(args, stdout, stderr)
	}
}

func selectCLIMode(executablePath, envMode string) (cliMode, error) {
	switch envMode {
	case "":
		// Fall through to executable-name detection.
	case "custom":
		return cliModeCustom, nil
	case "compat":
		return cliModeCompat, nil
	default:
		return cliModeCustom, fmt.Errorf("invalid ADB_GO_MODE %q: expected \"custom\" or \"compat\"", envMode)
	}

	switch filepath.Base(executablePath) {
	case "adb", "adb.exe":
		return cliModeCompat, nil
	default:
		return cliModeCustom, nil
	}
}
