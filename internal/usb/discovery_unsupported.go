//go:build !linux

package usb

import "context"

func DiscoverADB(ctx context.Context) ([]Candidate, error) {
	return nil, ErrUnsupported
}

func DiscoverADBInRoot(ctx context.Context, root string) ([]Candidate, error) {
	return nil, ErrUnsupported
}
