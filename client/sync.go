package client

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
)

const (
	syncIDRECV = "RECV"
	syncIDSEND = "SEND"
	syncIDDATA = "DATA"
	syncIDDONE = "DONE"
	syncIDOKAY = "OKAY"
	syncIDFAIL = "FAIL"

	// syncDataMax keeps each sync DATA packet comfortably within adb-go's v0
	// advertised ADB payload size. Future negotiated-max-payload plumbing can
	// raise this without changing the public PushFile API.
	syncDataMax = 4096
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

// PushFile pushes one local file to remotePath using ADB's sync: service. v0
// intentionally implements only explicit single-file pushes; directory-aware
// push behavior and custom mode/mtime options are reserved for future APIs.
func (c *Client) PushFile(ctx context.Context, localPath, remotePath string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if localPath == "" {
		return fmt.Errorf("adb push local path is empty")
	}
	if remotePath == "" {
		return fmt.Errorf("adb push remote path is empty")
	}

	in, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("adb push open source %q: %w", localPath, err)
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return fmt.Errorf("adb push stat source %q: %w", localPath, err)
	}
	if info.IsDir() {
		return fmt.Errorf("adb push source %q is a directory: %w", localPath, ErrUnsupported)
	}

	stream, err := c.OpenService(ctx, "sync:")
	if err != nil {
		return err
	}
	defer stream.Close()

	stopCancelCloser := closeOnCancel(ctx, stream)
	defer stopCancelCloser()

	// TODO(sync): Add explicit push options for remote mode and mtime. v0 uses
	// the adb-compatible default file mode requested in SPEC.md and the local
	// file's modification time.
	const defaultRemoteMode = 0o644
	sendPayload := []byte(remotePath + "," + strconv.FormatUint(defaultRemoteMode, 10))
	if err := writeSyncRequest(stream, syncIDSEND, sendPayload); err != nil {
		return fmt.Errorf("adb push %q to %q: send SEND: %w", localPath, remotePath, err)
	}

	buf := make([]byte, syncDataMax)
	for {
		n, readErr := in.Read(buf)
		if n > 0 {
			if err := writeSyncRequest(stream, syncIDDATA, buf[:n]); err != nil {
				return fmt.Errorf("adb push %q to %q: send DATA: %w", localPath, remotePath, err)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("adb push read source %q: %w", localPath, readErr)
		}
	}

	mtime := info.ModTime().Unix()
	if mtime < 0 {
		mtime = 0
	}
	if err := writeSyncHeader(stream, syncIDDONE, uint32(mtime)); err != nil {
		return fmt.Errorf("adb push %q to %q: send DONE: %w", localPath, remotePath, err)
	}

	id, size, err := readSyncHeader(stream)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("adb push %q to %q canceled: %w", localPath, remotePath, ctxErr)
		}
		return fmt.Errorf("adb push %q to %q: read response: %w", localPath, remotePath, err)
	}
	switch id {
	case syncIDOKAY:
		return nil
	case syncIDFAIL:
		msg := make([]byte, size)
		if _, err := io.ReadFull(stream, msg); err != nil {
			return fmt.Errorf("adb push %q to %q: read FAIL: %w", localPath, remotePath, err)
		}
		return fmt.Errorf("adb push %q to %q: remote error: %s", localPath, remotePath, string(msg))
	default:
		return fmt.Errorf("adb push %q to %q: unexpected sync response %q", localPath, remotePath, id)
	}
}

func writeSyncRequest(w io.Writer, id string, payload []byte) error {
	var header [8]byte
	writeSyncHeaderTo(header[:], id, uint32(len(payload)))
	packet := make([]byte, 0, len(header)+len(payload))
	packet = append(packet, header[:]...)
	packet = append(packet, payload...)
	return writeFull(w, packet)
}

func writeSyncHeader(w io.Writer, id string, size uint32) error {
	var header [8]byte
	writeSyncHeaderTo(header[:], id, size)
	return writeFull(w, header[:])
}

func writeFull(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, err := w.Write(p)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		p = p[n:]
	}
	return nil
}

func writeSyncHeaderTo(header []byte, id string, size uint32) {
	copy(header[0:4], id)
	binary.LittleEndian.PutUint32(header[4:8], size)
}

func readSyncHeader(r io.Reader) (id string, size uint32, err error) {
	var header [8]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return "", 0, err
	}
	return string(header[0:4]), binary.LittleEndian.Uint32(header[4:8]), nil
}
