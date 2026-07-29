package compat

import (
	"fmt"
	"io"
)

// Run is a temporary placeholder for adb-compatible mode. The router can select
// compat mode before the adb-shaped command implementation exists, which lets
// tests and users verify mode detection independently from the upcoming compat
// command skeleton.
func Run(args []string, stdout, stderr io.Writer) int {
	fmt.Fprintln(stderr, "adb-go compat mode is not implemented yet")
	return 1
}
