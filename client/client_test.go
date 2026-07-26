package client

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dector/adb-go/auth"
	"github.com/dector/adb-go/internal/fakeadb"
	"github.com/dector/adb-go/protocol"
)

func TestNormalizeTCPAddr(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "host without port", in: "127.0.0.1", want: "127.0.0.1:5555"},
		{name: "host with port", in: "127.0.0.1:1234", want: "127.0.0.1:1234"},
		{name: "hostname without port", in: "localhost", want: "localhost:5555"},
		{name: "ipv6 without port", in: "::1", want: "[::1]:5555"},
		{name: "bracketed ipv6 with port", in: "[::1]:1234", want: "[::1]:1234"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeTCPAddr(tt.in)
			if err != nil {
				t.Fatalf("normalizeTCPAddr() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("normalizeTCPAddr() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeTCPAddrEmpty(t *testing.T) {
	if _, err := normalizeTCPAddr("   "); err == nil {
		t.Fatal("normalizeTCPAddr(empty) error = nil, want error")
	}
}

func TestConnectSucceedsAgainstFakeServer(t *testing.T) {
	server := fakeadb.Start(t)

	client, err := Connect(context.Background(), server.Addr())
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestConnectWithTransportPerformsSharedHandshake(t *testing.T) {
	clientSide, serverSide := net.Pipe()
	defer serverSide.Close()

	done := make(chan error, 1)
	go func() {
		req, err := protocol.ReadMessage(serverSide)
		if err != nil {
			done <- err
			return
		}
		if req.Command != protocol.CommandCNXN {
			done <- errors.New("first client packet was not CNXN")
			return
		}
		done <- protocol.WriteMessage(serverSide, protocol.Message{
			Command: protocol.CommandCNXN,
			Arg0:    protocol.Version,
			Arg1:    protocol.MaxPayload,
			Payload: []byte("device::fake\x00"),
		})
	}()

	client, err := connectWithTransport(context.Background(), staticTransportDialer{
		desc: "test pipe",
		conn: clientSide,
	})
	if err != nil {
		t.Fatalf("connectWithTransport() error = %v", err)
	}
	defer client.Close()

	if err := <-done; err != nil {
		t.Fatalf("shared handshake server error = %v", err)
	}
}

func TestConnectTCPWithOptionsAuthenticatesAgainstFakeServer(t *testing.T) {
	server := fakeadb.Start(t)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	cred, err := auth.NewCredential(key)
	if err != nil {
		t.Fatalf("NewCredential: %v", err)
	}
	token := []byte("client token")
	digest := sha1.Sum(token)
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA1, digest[:])
	if err != nil {
		t.Fatalf("SignPKCS1v15: %v", err)
	}
	server.RequireAuth(token, signature, nil)

	client, err := ConnectTCPWithOptions(context.Background(), server.Addr(), ConnectOptions{AuthCredentials: []protocol.AuthCredential{cred}})
	if err != nil {
		t.Fatalf("ConnectTCPWithOptions() error = %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestConnectWithTransportMapsAuthResponse(t *testing.T) {
	clientSide, serverSide := net.Pipe()
	defer serverSide.Close()

	done := make(chan error, 1)
	go func() {
		if _, err := protocol.ReadMessage(serverSide); err != nil {
			done <- err
			return
		}
		done <- protocol.WriteMessage(serverSide, protocol.Message{
			Command: protocol.CommandAUTH,
			Arg0:    protocol.AuthToken,
			Payload: []byte("token"),
		})
	}()

	client, err := connectWithTransport(context.Background(), staticTransportDialer{
		desc: "test pipe",
		conn: clientSide,
	})
	if err == nil {
		_ = client.Close()
		t.Fatal("connectWithTransport() error = nil, want auth required")
	}
	if !errors.Is(err, ErrAuthRequired) {
		t.Fatalf("connectWithTransport() error = %v, want errors.Is ErrAuthRequired", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("auth handshake server error = %v", err)
	}
}

func TestConnectAuthResponse(t *testing.T) {
	addr, closeServer := startAuthServer(t)
	defer closeServer()

	client, err := Connect(context.Background(), addr)
	if err == nil {
		_ = client.Close()
		t.Fatal("Connect() error = nil, want auth required")
	}
	if !errors.Is(err, ErrAuthRequired) {
		t.Fatalf("Connect() error = %v, want errors.Is ErrAuthRequired", err)
	}
}

func TestOpenServiceAgainstFakeServer(t *testing.T) {
	server := fakeadb.Start(t)
	server.Handle("test:service", writeServiceOutput(t, "hello"))

	client, err := Connect(context.Background(), server.Addr())
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	stream, err := client.OpenService(context.Background(), "test:service")
	if err != nil {
		t.Fatalf("OpenService() error = %v", err)
	}
	defer stream.Close()

	buf := make([]byte, len("hello"))
	if _, err := io.ReadFull(stream, buf); err != nil {
		t.Fatalf("ReadFull() error = %v", err)
	}
	if string(buf) != "hello" {
		t.Fatalf("read payload = %q, want hello", string(buf))
	}
}

func TestShellCapturesOutputAgainstFakeServer(t *testing.T) {
	server := fakeadb.Start(t)
	server.Handle("shell:printf hello", writeServiceOutput(t, "hello\n"))

	client, err := Connect(context.Background(), server.Addr())
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	out, err := client.Shell(context.Background(), "printf hello")
	if err != nil {
		t.Fatalf("Shell() error = %v", err)
	}
	if string(out) != "hello\n" {
		t.Fatalf("Shell() output = %q, want hello newline", string(out))
	}
}

func TestShellStreamWritesToProvidedWriter(t *testing.T) {
	server := fakeadb.Start(t)
	server.Handle("shell:echo streamed", writeServiceOutput(t, "streamed\n"))

	client, err := Connect(context.Background(), server.Addr())
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	var out bytes.Buffer
	if err := client.ShellStream(context.Background(), "echo streamed", &out); err != nil {
		t.Fatalf("ShellStream() error = %v", err)
	}
	if out.String() != "streamed\n" {
		t.Fatalf("ShellStream() output = %q, want streamed newline", out.String())
	}
}

func TestShellUsesExactSingleCommandString(t *testing.T) {
	server := fakeadb.Start(t)
	opened := make(chan string, 1)
	wantService := "shell:pm list packages | grep example"
	server.Handle(wantService, func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		opened <- string(open.Payload[:len(open.Payload)-1])
		writeServiceOutput(t, "package:example\n")(ctx, conn, open)
	})

	client, err := Connect(context.Background(), server.Addr())
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	if _, err := client.Shell(context.Background(), "pm list packages | grep example"); err != nil {
		t.Fatalf("Shell() error = %v", err)
	}
	if got := <-opened; got != wantService {
		t.Fatalf("opened service = %q, want %q", got, wantService)
	}
}

func TestLogcatStreamsOutputAgainstFakeServer(t *testing.T) {
	server := fakeadb.Start(t)
	server.Handle("shell:logcat", writeServiceOutput(t, "01-02 03:04:05.678  123  456 I Tag: hello\n"))

	client, err := Connect(context.Background(), server.Addr())
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	var out bytes.Buffer
	if err := client.Logcat(context.Background(), &out, LogcatOptions{}); err != nil {
		t.Fatalf("Logcat() error = %v", err)
	}
	if out.String() != "01-02 03:04:05.678  123  456 I Tag: hello\n" {
		t.Fatalf("Logcat() output = %q, want fake log line", out.String())
	}
}

func TestLogcatDumpUsesDumpFlag(t *testing.T) {
	server := fakeadb.Start(t)
	opened := make(chan string, 1)
	server.Handle("shell:logcat -d", func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		opened <- string(open.Payload[:len(open.Payload)-1])
		writeServiceOutput(t, "dumped\n")(ctx, conn, open)
	})

	client, err := Connect(context.Background(), server.Addr())
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	var out bytes.Buffer
	if err := client.Logcat(context.Background(), &out, LogcatOptions{Dump: true}); err != nil {
		t.Fatalf("Logcat() error = %v", err)
	}
	if out.String() != "dumped\n" {
		t.Fatalf("Logcat() output = %q, want dumped newline", out.String())
	}
	if got := <-opened; got != "shell:logcat -d" {
		t.Fatalf("opened service = %q, want shell:logcat -d", got)
	}
}

func TestLogcatCommandOptions(t *testing.T) {
	if got := logcatCommand(LogcatOptions{}); got != "logcat" {
		t.Fatalf("logcatCommand(default) = %q, want logcat", got)
	}
	if got := logcatCommand(LogcatOptions{Dump: true}); got != "logcat -d" {
		t.Fatalf("logcatCommand(dump) = %q, want logcat -d", got)
	}
}

func TestLogcatRejectsNilWriter(t *testing.T) {
	server := fakeadb.Start(t)

	client, err := Connect(context.Background(), server.Addr())
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	if err := client.Logcat(context.Background(), nil, LogcatOptions{}); err == nil {
		t.Fatal("Logcat(nil writer) error = nil, want error")
	}
}

func TestShellStreamContextCancellationUnblocks(t *testing.T) {
	server := fakeadb.Start(t)
	server.Handle("shell:sleep forever", func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandOKAY, Arg0: 42, Arg1: open.Arg0})
		<-ctx.Done()
	})

	client, err := Connect(context.Background(), server.Addr())
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- client.ShellStream(ctx, "sleep forever", io.Discard)
	}()

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("ShellStream() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ShellStream() did not unblock after context cancellation")
	}
}

func TestPullFileWritesContentsFromFakeServer(t *testing.T) {
	server := fakeadb.Start(t)
	server.Handle("sync:", syncPullHandler(t, "/sdcard/hello.txt", nil, [][]byte{[]byte("hello "), []byte("world\n")}))

	client, err := Connect(context.Background(), server.Addr())
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	localPath := filepath.Join(t.TempDir(), "hello.txt")
	if err := client.PullFile(context.Background(), "/sdcard/hello.txt", localPath); err != nil {
		t.Fatalf("PullFile() error = %v", err)
	}
	got, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != "hello world\n" {
		t.Fatalf("pulled contents = %q, want hello world newline", string(got))
	}
}

func TestPullFileExistingDestinationWithoutOverwrite(t *testing.T) {
	server := fakeadb.Start(t)
	server.Handle("sync:", syncPullHandler(t, "/remote", nil, [][]byte{[]byte("new")}))

	client, err := Connect(context.Background(), server.Addr())
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	localPath := filepath.Join(t.TempDir(), "out.txt")
	if err := os.WriteFile(localPath, []byte("old"), 0o666); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	err = client.PullFile(context.Background(), "/remote", localPath)
	if !errors.Is(err, ErrDestinationExists) {
		t.Fatalf("PullFile() error = %v, want ErrDestinationExists", err)
	}
	got, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != "old" {
		t.Fatalf("existing file contents = %q, want old", string(got))
	}
}

func TestPullFileExistingDestinationWithOverwrite(t *testing.T) {
	server := fakeadb.Start(t)
	server.Handle("sync:", syncPullHandler(t, "/remote", nil, [][]byte{[]byte("new")}))

	client, err := Connect(context.Background(), server.Addr())
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	localPath := filepath.Join(t.TempDir(), "out.txt")
	if err := os.WriteFile(localPath, []byte("old"), 0o666); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := client.PullFileWithOptions(context.Background(), "/remote", localPath, PullOptions{Overwrite: true}); err != nil {
		t.Fatalf("PullFileWithOptions() error = %v", err)
	}
	got, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != "new" {
		t.Fatalf("overwritten file contents = %q, want new", string(got))
	}
}

func TestPullFileRemoteError(t *testing.T) {
	server := fakeadb.Start(t)
	server.Handle("sync:", syncPullHandler(t, "/missing", []byte("remote object does not exist"), nil))

	client, err := Connect(context.Background(), server.Addr())
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	localPath := filepath.Join(t.TempDir(), "missing.txt")
	err = client.PullFile(context.Background(), "/missing", localPath)
	if err == nil {
		t.Fatal("PullFile() error = nil, want remote error")
	}
	if _, statErr := os.Stat(localPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("destination exists after remote FAIL: stat error = %v", statErr)
	}
}

func TestPushFileSendsContentsToFakeServer(t *testing.T) {
	server := fakeadb.Start(t)
	result := make(chan syncPushResult, 1)
	server.Handle("sync:", syncPushHandler(t, "/data/local/tmp/out.txt", nil, result))

	client, err := Connect(context.Background(), server.Addr())
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	localPath := filepath.Join(t.TempDir(), "out.txt")
	if err := os.WriteFile(localPath, []byte("hello pushed file"), 0o666); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := client.PushFile(context.Background(), localPath, "/data/local/tmp/out.txt"); err != nil {
		t.Fatalf("PushFile() error = %v", err)
	}
	got := <-result
	if got.contents != "hello pushed file" {
		t.Fatalf("pushed contents = %q, want hello pushed file", got.contents)
	}
	if got.mode != "420" {
		t.Fatalf("remote mode = %q, want decimal 420 (0644)", got.mode)
	}
	if got.mtime == 0 {
		t.Fatal("DONE mtime = 0, want local file modification time")
	}
}

func TestInstallAPKPushesInstallsAndCleansUp(t *testing.T) {
	server := fakeadb.Start(t)
	remotePath := "/data/local/tmp/adb-go-install-test.apk"
	withInstallRemotePath(t, remotePath)

	pushResult := make(chan syncPushResult, 1)
	cleanup := make(chan struct{}, 1)
	server.Handle("sync:", syncPushHandler(t, remotePath, nil, pushResult))
	server.Handle("shell:pm install -r '"+remotePath+"'", writeServiceOutput(t, "Success\n"))
	server.Handle("shell:rm -f -- '"+remotePath+"'", func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		cleanup <- struct{}{}
		writeServiceOutput(t, "")(ctx, conn, open)
	})

	client, err := Connect(context.Background(), server.Addr())
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	localPath := filepath.Join(t.TempDir(), "app.apk")
	if err := os.WriteFile(localPath, []byte("apk bytes"), 0o666); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := client.InstallAPKWithOptions(context.Background(), localPath, InstallOptions{Replace: true}); err != nil {
		t.Fatalf("InstallAPKWithOptions() error = %v", err)
	}
	if got := <-pushResult; got.contents != "apk bytes" {
		t.Fatalf("pushed APK contents = %q, want apk bytes", got.contents)
	}
	select {
	case <-cleanup:
	case <-time.After(time.Second):
		t.Fatal("cleanup shell command was not opened")
	}
}

func TestInstallAPKPackageManagerFailureIncludesOutput(t *testing.T) {
	server := fakeadb.Start(t)
	remotePath := "/data/local/tmp/adb-go-install-fail.apk"
	withInstallRemotePath(t, remotePath)

	cleanup := make(chan struct{}, 1)
	server.Handle("sync:", syncPushHandler(t, remotePath, nil, nil))
	server.Handle("shell:pm install '"+remotePath+"'", writeServiceOutput(t, "Failure [INSTALL_FAILED_VERSION_DOWNGRADE]\n"))
	server.Handle("shell:rm -f -- '"+remotePath+"'", func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		cleanup <- struct{}{}
		writeServiceOutput(t, "")(ctx, conn, open)
	})

	client, err := Connect(context.Background(), server.Addr())
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	localPath := filepath.Join(t.TempDir(), "app.apk")
	if err := os.WriteFile(localPath, []byte("apk bytes"), 0o666); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	err = client.InstallAPK(context.Background(), localPath)
	if err == nil {
		t.Fatal("InstallAPK() error = nil, want package manager failure")
	}
	if !strings.Contains(err.Error(), "INSTALL_FAILED_VERSION_DOWNGRADE") {
		t.Fatalf("InstallAPK() error = %v, want package manager output", err)
	}
	select {
	case <-cleanup:
	case <-time.After(time.Second):
		t.Fatal("cleanup shell command was not opened after install failure")
	}
}

func TestPushFileMissingLocalFile(t *testing.T) {
	server := fakeadb.Start(t)

	client, err := Connect(context.Background(), server.Addr())
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	err = client.PushFile(context.Background(), filepath.Join(t.TempDir(), "missing.txt"), "/remote")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("PushFile() error = %v, want os.ErrNotExist", err)
	}
}

func TestPushFileRemoteError(t *testing.T) {
	server := fakeadb.Start(t)
	server.Handle("sync:", syncPushHandler(t, "/readonly/out.txt", []byte("remote read-only file system"), nil))

	client, err := Connect(context.Background(), server.Addr())
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	localPath := filepath.Join(t.TempDir(), "out.txt")
	if err := os.WriteFile(localPath, []byte("data"), 0o666); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	err = client.PushFile(context.Background(), localPath, "/readonly/out.txt")
	if err == nil {
		t.Fatal("PushFile() error = nil, want remote error")
	}
}

func withInstallRemotePath(t testing.TB, remotePath string) {
	t.Helper()
	old := makeInstallRemotePath
	makeInstallRemotePath = func(string) string { return remotePath }
	t.Cleanup(func() { makeInstallRemotePath = old })
}

func writeServiceOutput(t testing.TB, output string) fakeadb.ServiceHandler {
	t.Helper()
	return func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		remoteID := uint32(42)
		_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandOKAY, Arg0: remoteID, Arg1: open.Arg0})
		_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandWRTE, Arg0: remoteID, Arg1: open.Arg0, Payload: []byte(output)})
		_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandCLSE, Arg0: remoteID, Arg1: open.Arg0})
	}
}

func syncPullHandler(t testing.TB, wantPath string, fail []byte, chunks [][]byte) fakeadb.ServiceHandler {
	t.Helper()
	return func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		remoteID := uint32(42)
		_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandOKAY, Arg0: remoteID, Arg1: open.Arg0})

		request, err := protocol.ReadMessage(conn)
		if err != nil {
			t.Errorf("sync handler ReadMessage() error = %v", err)
			return
		}
		if request.Command != protocol.CommandWRTE {
			t.Errorf("sync handler command = %#x, want WRTE", uint32(request.Command))
			return
		}
		_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandOKAY, Arg0: remoteID, Arg1: open.Arg0})

		id, path := parseSyncRequest(t, request.Payload)
		if id != "RECV" {
			t.Errorf("sync request id = %q, want RECV", id)
			return
		}
		if path != wantPath {
			t.Errorf("sync request path = %q, want %q", path, wantPath)
			return
		}

		if fail != nil {
			_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandWRTE, Arg0: remoteID, Arg1: open.Arg0, Payload: syncPacket("FAIL", fail)})
			_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandCLSE, Arg0: remoteID, Arg1: open.Arg0})
			return
		}
		for _, chunk := range chunks {
			_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandWRTE, Arg0: remoteID, Arg1: open.Arg0, Payload: syncPacket("DATA", chunk)})
		}
		_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandWRTE, Arg0: remoteID, Arg1: open.Arg0, Payload: syncPacket("DONE", nil)})
		_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandCLSE, Arg0: remoteID, Arg1: open.Arg0})
	}
}

type syncPushResult struct {
	contents string
	mode     string
	mtime    uint32
}

func syncPushHandler(t testing.TB, wantPath string, fail []byte, result chan<- syncPushResult) fakeadb.ServiceHandler {
	t.Helper()
	return func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		remoteID := uint32(42)
		_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandOKAY, Arg0: remoteID, Arg1: open.Arg0})

		var got syncPushResult
		for {
			request, err := protocol.ReadMessage(conn)
			if err != nil {
				t.Errorf("sync push handler ReadMessage() error = %v", err)
				return
			}
			if request.Command != protocol.CommandWRTE {
				t.Errorf("sync push handler command = %#x, want WRTE", uint32(request.Command))
				return
			}
			_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandOKAY, Arg0: remoteID, Arg1: open.Arg0})

			id, size, payload := parseSyncPacket(t, request.Payload)
			switch id {
			case "SEND":
				pathMode := string(payload)
				comma := bytes.LastIndexByte(payload, ',')
				if comma < 0 {
					t.Fatalf("SEND payload = %q, want path,mode", pathMode)
				}
				path := string(payload[:comma])
				got.mode = string(payload[comma+1:])
				if path != wantPath {
					t.Errorf("SEND path = %q, want %q", path, wantPath)
					return
				}
			case "DATA":
				got.contents += string(payload)
			case "DONE":
				got.mtime = size
				response := syncPacket("OKAY", nil)
				if fail != nil {
					response = syncPacket("FAIL", fail)
				}
				_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandWRTE, Arg0: remoteID, Arg1: open.Arg0, Payload: response})
				_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandCLSE, Arg0: remoteID, Arg1: open.Arg0})
				if result != nil {
					result <- got
				}
				return
			default:
				t.Errorf("sync push request id = %q, want SEND/DATA/DONE", id)
				return
			}
		}
	}
}

func parseSyncRequest(t testing.TB, payload []byte) (id, path string) {
	t.Helper()
	id, n, data := parseSyncPacket(t, payload)
	if len(data) != int(n) {
		t.Fatalf("sync request path length = %d, want %d", len(data), n)
	}
	return id, string(data)
}

func parseSyncPacket(t testing.TB, payload []byte) (id string, size uint32, data []byte) {
	t.Helper()
	if len(payload) < 8 {
		t.Fatalf("sync packet payload length = %d, want at least 8", len(payload))
	}
	id = string(payload[:4])
	size = binary.LittleEndian.Uint32(payload[4:8])
	data = payload[8:]
	if id != "DONE" && len(data) != int(size) {
		t.Fatalf("sync packet data length = %d, want %d", len(data), size)
	}
	return id, size, data
}

func TestWriteSyncRequestCompletesShortWrites(t *testing.T) {
	var dst bytes.Buffer
	payload := []byte("/sdcard/chunked.txt")

	if err := writeSyncRequest(shortWriter{w: &dst, max: 3}, syncIDRECV, payload); err != nil {
		t.Fatalf("writeSyncRequest() error = %v", err)
	}

	id, size, data := parseSyncPacket(t, dst.Bytes())
	if id != syncIDRECV {
		t.Fatalf("sync id = %q, want %q", id, syncIDRECV)
	}
	if size != uint32(len(payload)) || !bytes.Equal(data, payload) {
		t.Fatalf("sync payload size=%d data=%q, want size=%d data=%q", size, data, len(payload), payload)
	}
}

func TestWriteSyncHeaderCompletesShortWrites(t *testing.T) {
	var dst bytes.Buffer

	if err := writeSyncHeader(shortWriter{w: &dst, max: 2}, syncIDDONE, 123); err != nil {
		t.Fatalf("writeSyncHeader() error = %v", err)
	}

	id, size, data := parseSyncPacket(t, dst.Bytes())
	if id != syncIDDONE || size != 123 || len(data) != 0 {
		t.Fatalf("sync header id=%q size=%d data=%q, want DONE/123/no data", id, size, data)
	}
}

type staticTransportDialer struct {
	desc string
	conn io.ReadWriteCloser
}

func (d staticTransportDialer) DialTransport(ctx context.Context) (io.ReadWriteCloser, error) {
	return d.conn, nil
}

func (d staticTransportDialer) ConnectDescription() string {
	return d.desc
}

type shortWriter struct {
	w   io.Writer
	max int
}

func (w shortWriter) Write(p []byte) (int, error) {
	if len(p) > w.max {
		p = p[:w.max]
	}
	return w.w.Write(p)
}

func syncPacket(id string, payload []byte) []byte {
	packet := make([]byte, 8+len(payload))
	copy(packet[:4], id)
	binary.LittleEndian.PutUint32(packet[4:8], uint32(len(payload)))
	copy(packet[8:], payload)
	return packet
}

func startAuthServer(t *testing.T) (addr string, closeServer func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = protocol.ReadMessage(conn)
		_ = protocol.WriteMessage(conn, protocol.Message{
			Command: protocol.CommandAUTH,
			Arg0:    protocol.AuthToken,
			Payload: []byte("token"),
		})
	}()
	return ln.Addr().String(), func() {
		_ = ln.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("auth server did not stop")
		}
	}
}
