package custom

import (
	"context"
	"flag"
	"fmt"
	"io"

	adb "github.com/dector/adb-go"
)

const pushUsage = `Usage:
  adb-go push (--addr HOST[:PORT] | --usb [USB selection]) LOCAL_PATH REMOTE_PATH

Pushes exactly one local file to the selected ADB device. TCP addresses come from
--addr, or from ADB_GO_ADDR when --addr is omitted. USB support is Linux-only
initially. For example:

  adb-go push --addr 127.0.0.1:5555 ./local.txt /data/local/tmp/local.txt
  adb-go push --auth-key ~/.android/adbkey --usb-path /dev/bus/usb/001/002 ./local.txt /data/local/tmp/local.txt
`

func runPush(args []string, stdout, stderr io.Writer) int {
	return runPushWithOptions(args, cliOptions{}, stdout, stderr)
}

func runPushWithOptions(args []string, opts cliOptions, stdout, stderr io.Writer) int {
	out := newOutputPolicy(stdout, stderr, opts)
	fs := flag.NewFlagSet("push", flag.ContinueOnError)
	fs.SetOutput(stderr)
	conn := addConnectionFlags(fs)
	fs.Usage = func() { fmt.Fprint(stderr, pushUsage) }

	if err := fs.Parse(args); err != nil {
		return 2
	}
	target, err := conn.target(fs)
	if err != nil {
		fmt.Fprintf(stderr, "adb-go push: %v\n\n", err)
		fs.Usage()
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprint(stderr, "adb-go push: requires exactly LOCAL_PATH and REMOTE_PATH\n\n")
		fs.Usage()
		return 2
	}

	localPath, remotePath := fs.Arg(0), fs.Arg(1)
	out.Verbosef("push transferring %s to %s\n", localPath, remotePath)
	client, err := connectTarget(context.Background(), "push", target, out)
	if err != nil {
		printConnectError(stderr, "push", target.description, err)
		return 1
	}
	defer client.Close()

	if err := client.PushFile(context.Background(), localPath, remotePath); err != nil {
		printCommandError(stderr, "push", err)
		return 1
	}
	return 0
}

const pullUsage = `Usage:
  adb-go pull (--addr HOST[:PORT] | --usb [USB selection]) [--overwrite] REMOTE_PATH LOCAL_PATH

Pulls exactly one remote file from the selected ADB device. TCP addresses come
from --addr, or from ADB_GO_ADDR when --addr is omitted. USB support is
Linux-only initially. By default, the command refuses to replace an existing
local destination; pass --overwrite to replace it deliberately. For example:

  adb-go pull --addr 127.0.0.1:5555 /data/local/tmp/remote.txt ./remote.txt
  adb-go pull --auth-key ~/.android/adbkey --usb-path /dev/bus/usb/001/002 /data/local/tmp/remote.txt ./remote.txt
`

const installAPKUsage = `Usage:
  adb-go install-apk (--addr HOST[:PORT] | --usb [USB selection]) [--replace] LOCAL_APK

Installs exactly one local APK on the selected ADB device. This is an
adb-go-specific helper, not a full clone of "adb install". It pushes the APK to
a temporary path under /data/local/tmp, runs pm install, and asks the device to
remove the temporary APK afterward. TCP addresses come from --addr, or from
ADB_GO_ADDR when --addr is omitted. USB support is Linux-only initially. Pass
--replace to allow replacing an already-installed app via pm install -r. For
example:

  adb-go install-apk --addr 127.0.0.1:5555 ./app.apk
  adb-go install-apk --replace --auth-key ~/.android/adbkey --usb-path /dev/bus/usb/001/002 ./app.apk
`

func runInstallAPK(args []string, stdout, stderr io.Writer) int {
	return runInstallAPKWithOptions(args, cliOptions{}, stdout, stderr)
}

func runInstallAPKWithOptions(args []string, opts cliOptions, stdout, stderr io.Writer) int {
	out := newOutputPolicy(stdout, stderr, opts)
	fs := flag.NewFlagSet("install-apk", flag.ContinueOnError)
	fs.SetOutput(stderr)
	conn := addConnectionFlags(fs)
	replace := fs.Bool("replace", false, "allow package replacement with pm install -r")
	fs.Usage = func() { fmt.Fprint(stderr, installAPKUsage) }

	if err := fs.Parse(args); err != nil {
		return 2
	}
	target, err := conn.target(fs)
	if err != nil {
		fmt.Fprintf(stderr, "adb-go install-apk: %v\n\n", err)
		fs.Usage()
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprint(stderr, "adb-go install-apk: requires exactly LOCAL_APK\n\n")
		fs.Usage()
		return 2
	}

	localPath := fs.Arg(0)
	out.Verbosef("install-apk installing %s (replace=%t)\n", localPath, *replace)
	client, err := connectTarget(context.Background(), "install-apk", target, out)
	if err != nil {
		printConnectError(stderr, "install-apk", target.description, err)
		return 1
	}
	defer client.Close()

	if *replace {
		err = client.InstallAPKWithOptions(context.Background(), localPath, adb.InstallOptions{Replace: true})
	} else {
		err = client.InstallAPK(context.Background(), localPath)
	}
	if err != nil {
		printCommandError(stderr, "install-apk", err)
		return 1
	}
	return 0
}

func runPull(args []string, stdout, stderr io.Writer) int {
	return runPullWithOptions(args, cliOptions{}, stdout, stderr)
}

func runPullWithOptions(args []string, opts cliOptions, stdout, stderr io.Writer) int {
	out := newOutputPolicy(stdout, stderr, opts)
	fs := flag.NewFlagSet("pull", flag.ContinueOnError)
	fs.SetOutput(stderr)
	conn := addConnectionFlags(fs)
	overwrite := fs.Bool("overwrite", false, "replace an existing local destination")
	fs.Usage = func() { fmt.Fprint(stderr, pullUsage) }

	if err := fs.Parse(args); err != nil {
		return 2
	}
	target, err := conn.target(fs)
	if err != nil {
		fmt.Fprintf(stderr, "adb-go pull: %v\n\n", err)
		fs.Usage()
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprint(stderr, "adb-go pull: requires exactly REMOTE_PATH and LOCAL_PATH\n\n")
		fs.Usage()
		return 2
	}

	remotePath, localPath := fs.Arg(0), fs.Arg(1)
	out.Verbosef("pull transferring %s to %s (overwrite=%t)\n", remotePath, localPath, *overwrite)
	client, err := connectTarget(context.Background(), "pull", target, out)
	if err != nil {
		printConnectError(stderr, "pull", target.description, err)
		return 1
	}
	defer client.Close()

	if *overwrite {
		err = client.PullFileWithOptions(context.Background(), remotePath, localPath, adb.PullOptions{Overwrite: true})
	} else {
		err = client.PullFile(context.Background(), remotePath, localPath)
	}
	if err != nil {
		printCommandError(stderr, "pull", err)
		return 1
	}
	return 0
}
