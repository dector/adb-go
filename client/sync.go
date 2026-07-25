package client

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	syncIDRECV = "RECV"
	syncIDDATA = "DATA"
	syncIDDONE = "DONE"
	syncIDFAIL = "FAIL"
)

// PullOptions controls PullFileWithOptions behavior.
type PullOptions struct {
	// Overwrite allows PullFileWithOptions to replace an existing local file.
	Overwrite bool
}

// PullFile pulls one remote file to localPath and fails if localPath already
// exists. Use PullFileWithOptions with PullOptions{Overwrite: true} to replace
// an existing file deliberately.
func (c *Client) PullFile(ctx context.Context, remotePath, localPath string) error {
	return c.PullFileWithOptions(ctx, remotePath, localPath, PullOptions{})
}

// PullFileWithOptions pulls one remote file to localPath using ADB's sync:
// service. Directory-aware pull behavior is intentionally reserved for future
// APIs; v0 implements only explicit single-file pulls.
func (c *Client) PullFileWithOptions(ctx context.Context, remotePath, localPath string, opts PullOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if remotePath == "" {
		return fmt.Errorf("adb pull remote path is empty")
	}
	if localPath == "" {
		return fmt.Errorf("adb pull local path is empty")
	}
	if !opts.Overwrite {
		if _, err := os.Stat(localPath); err == nil {
			return fmt.Errorf("adb pull destination %q exists: %w", localPath, ErrDestinationExists)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("adb pull stat destination %q: %w", localPath, err)
		}
	}

	stream, err := c.OpenService(ctx, "sync:")
	if err != nil {
		return err
	}
	defer stream.Close()

	stopCancelCloser := closeOnCancel(ctx, stream)
	defer stopCancelCloser()

	if err := writeSyncRequest(stream, syncIDRECV, []byte(remotePath)); err != nil {
		return fmt.Errorf("adb pull %q: send RECV: %w", remotePath, err)
	}

	var out *os.File
	created := false
	defer func() {
		if out != nil {
			_ = out.Close()
		}
	}()

	createOut := func() error {
		if created {
			return nil
		}
		flag := os.O_WRONLY | os.O_CREATE
		if opts.Overwrite {
			flag |= os.O_TRUNC
		} else {
			flag |= os.O_EXCL
		}
		f, err := os.OpenFile(localPath, flag, 0o666)
		if err != nil {
			if errors.Is(err, os.ErrExist) {
				return fmt.Errorf("adb pull destination %q exists: %w", localPath, ErrDestinationExists)
			}
			return fmt.Errorf("adb pull create destination %q: %w", localPath, err)
		}
		out = f
		created = true
		return nil
	}

	for {
		id, size, err := readSyncHeader(stream)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return fmt.Errorf("adb pull %q canceled: %w", remotePath, ctxErr)
			}
			return fmt.Errorf("adb pull %q: read response: %w", remotePath, err)
		}

		switch id {
		case syncIDDATA:
			if err := createOut(); err != nil {
				return err
			}
			if _, err := io.CopyN(out, stream, int64(size)); err != nil {
				return fmt.Errorf("adb pull %q: read DATA: %w", remotePath, err)
			}
		case syncIDDONE:
			if err := createOut(); err != nil {
				return err
			}
			if err := out.Close(); err != nil {
				out = nil
				return fmt.Errorf("adb pull close destination %q: %w", localPath, err)
			}
			out = nil
			return nil
		case syncIDFAIL:
			msg := make([]byte, size)
			if _, err := io.ReadFull(stream, msg); err != nil {
				return fmt.Errorf("adb pull %q: read FAIL: %w", remotePath, err)
			}
			return fmt.Errorf("adb pull %q: remote error: %s", remotePath, string(msg))
		default:
			return fmt.Errorf("adb pull %q: unexpected sync response %q", remotePath, id)
		}
	}
}

func writeSyncRequest(w io.Writer, id string, payload []byte) error {
	var header [8]byte
	copy(header[0:4], id)
	binary.LittleEndian.PutUint32(header[4:8], uint32(len(payload)))
	packet := make([]byte, 0, len(header)+len(payload))
	packet = append(packet, header[:]...)
	packet = append(packet, payload...)
	_, err := w.Write(packet)
	return err
}

func readSyncHeader(r io.Reader) (id string, size uint32, err error) {
	var header [8]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return "", 0, err
	}
	return string(header[0:4]), binary.LittleEndian.Uint32(header[4:8]), nil
}
