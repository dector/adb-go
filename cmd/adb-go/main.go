package main

import (
	"os"

	"github.com/dector/adb-go/cmd/adb-go/internal/compat"
	"github.com/dector/adb-go/cmd/adb-go/internal/custom"
)

// version is intentionally a package variable so release builds can inject a
// concrete value with Go's standard linker flag. tools/git-version.sh derives
// the project convention from Git tags for release/snapshot builds.
var version = "dev"

func main() {
	custom.Version = version
	os.Exit(runCLI(os.Args[1:], os.Stdout, os.Stderr, os.Args[0], os.Getenv("ADB_GO_MODE"), custom.Run, compat.Run))
}
