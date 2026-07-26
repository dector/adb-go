package client

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dector/adb-go/internal/fakeadb"
)

func TestScreencapCapturesPNGBytesAgainstFakeServer(t *testing.T) {
	server := fakeadb.Start(t)
	png := append([]byte(nil), pngSignature...)
	png = append(png, []byte("fake png payload")...)
	server.Handle("shell:screencap -p", writeServiceOutput(t, string(png)))

	client, err := Connect(context.Background(), server.Addr())
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	got, err := client.Screencap(context.Background())
	if err != nil {
		t.Fatalf("Screencap() error = %v", err)
	}
	if !bytes.Equal(got, png) {
		t.Fatalf("Screencap() bytes = %q, want %q", got, png)
	}
}

func TestScreencapNormalizesCRLFMangledPNG(t *testing.T) {
	mangled := []byte{0x89, 'P', 'N', 'G', '\r', '\r', '\n', 0x1a, '\n', 'a', '\r', '\n', 'b'}
	want := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 'a', '\n', 'b'}

	got := normalizeScreencapPNG(mangled)
	if !bytes.Equal(got, want) {
		t.Fatalf("normalizeScreencapPNG() = %q, want %q", got, want)
	}
}

func TestScreencapFileWritesNewDestination(t *testing.T) {
	server := fakeadb.Start(t)
	png := append([]byte(nil), pngSignature...)
	png = append(png, []byte("file payload")...)
	server.Handle("shell:screencap -p", writeServiceOutput(t, string(png)))

	client, err := Connect(context.Background(), server.Addr())
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	localPath := filepath.Join(t.TempDir(), "screen.png")
	if err := client.ScreencapFile(context.Background(), localPath); err != nil {
		t.Fatalf("ScreencapFile() error = %v", err)
	}
	got, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !bytes.Equal(got, png) {
		t.Fatalf("written screencap = %q, want %q", got, png)
	}
}

func TestScreencapFileExistingDestination(t *testing.T) {
	server := fakeadb.Start(t)
	server.Handle("shell:screencap -p", writeServiceOutput(t, string(pngSignature)))

	client, err := Connect(context.Background(), server.Addr())
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	localPath := filepath.Join(t.TempDir(), "screen.png")
	if err := os.WriteFile(localPath, []byte("old"), 0o666); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	err = client.ScreencapFile(context.Background(), localPath)
	if !errors.Is(err, ErrDestinationExists) {
		t.Fatalf("ScreencapFile() error = %v, want ErrDestinationExists", err)
	}
	got, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != "old" {
		t.Fatalf("existing file contents = %q, want old", got)
	}
}
