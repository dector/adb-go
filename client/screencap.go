package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
)

var pngSignature = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

// Screencap captures one PNG screenshot from the connected device by running
// the Android "screencap -p" shell command.
//
// Some Android shell paths historically pass binary shell output through a
// pseudo-terminal that expands LF bytes to CRLF. When adb-go recognizes that
// specific transformation in the PNG signature, it removes the inserted CR
// bytes so callers receive normal PNG data.
func (c *Client) Screencap(ctx context.Context) ([]byte, error) {
	out, err := c.Shell(ctx, "screencap -p")
	if err != nil {
		return nil, fmt.Errorf("adb screencap: %w", err)
	}
	return normalizeScreencapPNG(out), nil
}

// ScreencapFile captures one PNG screenshot from the connected device and
// writes it to localPath. The destination must not already exist.
func (c *Client) ScreencapFile(ctx context.Context, localPath string) error {
	png, err := c.Screencap(ctx)
	if err != nil {
		return err
	}

	file, err := os.OpenFile(localPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("adb screencap destination %q: %w", localPath, ErrDestinationExists)
		}
		return fmt.Errorf("adb screencap destination %q: %w", localPath, err)
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(localPath)
		}
	}()

	if _, err := file.Write(png); err != nil {
		_ = file.Close()
		return fmt.Errorf("write adb screencap destination %q: %w", localPath, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close adb screencap destination %q: %w", localPath, err)
	}
	ok = true
	return nil
}

func normalizeScreencapPNG(in []byte) []byte {
	if bytes.HasPrefix(in, pngSignature) {
		return in
	}

	// A CRLF-mangled PNG starts with \x89PNG\r\r\n\x1a\n: the original PNG
	// signature already contains CRLF, and the shell path inserted one extra CR
	// before the LF. Only apply the compatibility cleanup when this signature is
	// visible so arbitrary command output is not rewritten unexpectedly.
	mangledSignature := []byte{0x89, 'P', 'N', 'G', '\r', '\r', '\n', 0x1a, '\n'}
	if !bytes.HasPrefix(in, mangledSignature) {
		return in
	}

	out := make([]byte, 0, len(in))
	for i := 0; i < len(in); i++ {
		if in[i] == '\r' && i+1 < len(in) && in[i+1] == '\n' {
			continue
		}
		out = append(out, in[i])
	}
	return out
}
