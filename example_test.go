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

	if err := client.ShellStream(ctx, "pm list packages", os.Stdout); err != nil {
		return
	}
}

func ExampleClient_Logcat() {
	ctx := context.Background()
	client, err := adb.Connect(ctx, "127.0.0.1:5555")
	if err != nil {
		return
	}
	defer client.Close()

	if err := client.Logcat(ctx, os.Stdout, adb.LogcatOptions{Dump: true}); err != nil {
		return
	}
}

func ExampleClient_GetProp() {
	ctx := context.Background()
	client, err := adb.Connect(ctx, "127.0.0.1:5555")
	if err != nil {
		return
	}
	defer client.Close()

	sdk, err := client.GetProp(ctx, "ro.build.version.sdk")
	if err != nil {
		return
	}
	fmt.Println(sdk)
}

func ExampleClient_Properties() {
	ctx := context.Background()
	client, err := adb.Connect(ctx, "127.0.0.1:5555")
	if err != nil {
		return
	}
	defer client.Close()

	props, err := client.Properties(ctx)
	if err != nil {
		return
	}
	fmt.Println(props["ro.product.model"])
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

func ExampleClient_InstallAPK() {
	ctx := context.Background()
	client, err := adb.Connect(ctx, "127.0.0.1:5555")
	if err != nil {
		return
	}
	defer client.Close()

	if err := client.InstallAPK(ctx, "./app.apk"); err != nil {
		return
	}
}

func ExampleClient_InstallAPKWithOptions() {
	ctx := context.Background()
	client, err := adb.Connect(ctx, "127.0.0.1:5555")
	if err != nil {
		return
	}
	defer client.Close()

	err = client.InstallAPKWithOptions(ctx, "./app.apk", adb.InstallOptions{Replace: true})
	if err != nil {
		return
	}
}

func ExampleClient_Screencap() {
	ctx := context.Background()
	client, err := adb.Connect(ctx, "127.0.0.1:5555")
	if err != nil {
		return
	}
	defer client.Close()

	png, err := client.Screencap(ctx)
	if err != nil {
		return
	}
	fmt.Printf("captured %d bytes\n", len(png))
}

func ExampleClient_ScreencapFile() {
	ctx := context.Background()
	client, err := adb.Connect(ctx, "127.0.0.1:5555")
	if err != nil {
		return
	}
	defer client.Close()

	if err := client.ScreencapFile(ctx, "./screen.png"); err != nil {
		return
	}
}

func ExampleClient_Reboot() {
	ctx := context.Background()
	client, err := adb.Connect(ctx, "127.0.0.1:5555")
	if err != nil {
		return
	}
	defer client.Close()

	if err := client.Reboot(ctx, adb.RebootRecovery); err != nil {
		return
	}
}

func ExampleClient_ForwardLocalTCP() {
	ctx := context.Background()
	client, err := adb.Connect(ctx, "127.0.0.1:5555")
	if err != nil {
		return
	}
	defer client.Close()

	remote, err := adb.ForwardTCP(8080)
	if err != nil {
		return
	}
	forward, err := client.ForwardLocalTCP(ctx, "127.0.0.1:0", remote)
	if err != nil {
		return
	}
	defer forward.Close()
	go func() { _ = forward.Wait() }()

	fmt.Println("listening on", forward.LocalAddr())
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
