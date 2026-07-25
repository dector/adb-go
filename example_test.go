package adb_test

import (
	"context"
	"fmt"
	"io"
	"os"

	adb "github.com/dector/adb-go"
)

func ExampleConnect() {
	ctx := context.Background()

	client, err := adb.Connect(ctx, "127.0.0.1:5555")
	if err != nil {
		// Handle connection, authentication, or transport errors.
		return
	}
	defer client.Close()
}

func ExampleConnectTCPWithOptions_authentication() {
	ctx := context.Background()

	credential, err := adb.LoadPrivateKey("/home/me/.android/adbkey")
	if err != nil {
		return
	}
	client, err := adb.ConnectTCPWithOptions(ctx, "127.0.0.1:5555", adb.ConnectOptions{
		AuthCredentials: []adb.AuthCredential{credential},
	})
	if err != nil {
		return
	}
	defer client.Close()
}

func ExampleClient_Shell() {
	ctx := context.Background()
	client, err := adb.Connect(ctx, "127.0.0.1:5555")
	if err != nil {
		return
	}
	defer client.Close()

	out, err := client.Shell(ctx, "echo hello")
	if err != nil {
		return
	}
	fmt.Printf("%s", out)
}

func ExampleClient_ShellStream() {
	ctx := context.Background()
	client, err := adb.Connect(ctx, "127.0.0.1:5555")
	if err != nil {
		return
	}
	defer client.Close()

	if err := client.ShellStream(ctx, "logcat -d", os.Stdout); err != nil {
		return
	}
}

func ExampleClient_PushFile() {
	ctx := context.Background()
	client, err := adb.Connect(ctx, "127.0.0.1:5555")
	if err != nil {
		return
	}
	defer client.Close()

	if err := client.PushFile(ctx, "./local.txt", "/data/local/tmp/local.txt"); err != nil {
		return
	}
}

func ExampleClient_PullFile() {
	ctx := context.Background()
	client, err := adb.Connect(ctx, "127.0.0.1:5555")
	if err != nil {
		return
	}
	defer client.Close()

	if err := client.PullFile(ctx, "/data/local/tmp/remote.txt", "./remote.txt"); err != nil {
		return
	}
}

func ExampleClient_PullFileWithOptions() {
	ctx := context.Background()
	client, err := adb.Connect(ctx, "127.0.0.1:5555")
	if err != nil {
		return
	}
	defer client.Close()

	err = client.PullFileWithOptions(ctx,
		"/data/local/tmp/remote.txt",
		"./remote.txt",
		adb.PullOptions{Overwrite: true},
	)
	if err != nil {
		return
	}
}

func ExampleClient_OpenService() {
	ctx := context.Background()
	client, err := adb.Connect(ctx, "127.0.0.1:5555")
	if err != nil {
		return
	}
	defer client.Close()

	stream, err := client.OpenService(ctx, "shell:uname -a")
	if err != nil {
		return
	}
	defer stream.Close()

	_, _ = io.Copy(os.Stdout, stream)
}
