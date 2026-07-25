//go:build !linux

package usb

import (
	"context"
	"io"
)

func OpenBulkTransport(ctx context.Context, candidate Candidate) (io.ReadWriteCloser, error) {
	return nil, ErrUnsupported
}
