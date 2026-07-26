package client

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestScanTCPTargetsFindsConnectedAndAuthRequiredTargets(t *testing.T) {
	restore := replaceScanTCPConnect(func(ctx context.Context, addr string, opts ConnectOptions) (*Client, error) {
		switch addr {
		case "127.0.0.1:5555":
			return &Client{}, nil
		case "127.0.0.1:5557":
			return nil, fmt.Errorf("wrapped: %w", ErrAuthRequired)
		default:
			return nil, errors.New("connection refused")
		}
	})
	defer restore()

	targets, err := ScanTCPTargets(context.Background(), TCPScanOptions{StartPort: 5554, EndPort: 5558, Timeout: time.Second})
	if err != nil {
		t.Fatalf("ScanTCPTargets() error = %v", err)
	}

	if len(targets) != 2 {
		t.Fatalf("targets = %#v, want 2 targets", targets)
	}
	if targets[0].Addr != "127.0.0.1:5555" || targets[0].AuthRequired {
		t.Fatalf("targets[0] = %#v, want connected 5555", targets[0])
	}
	if targets[1].Addr != "127.0.0.1:5557" || !targets[1].AuthRequired {
		t.Fatalf("targets[1] = %#v, want auth-required 5557", targets[1])
	}
}

func TestScanTCPTargetsDefaultsToOddLocalhostEmulatorPorts(t *testing.T) {
	var mu sync.Mutex
	var addrs []string
	restore := replaceScanTCPConnect(func(ctx context.Context, addr string, opts ConnectOptions) (*Client, error) {
		mu.Lock()
		addrs = append(addrs, addr)
		mu.Unlock()
		return nil, errors.New("connection refused")
	})
	defer restore()

	_, err := ScanTCPTargets(context.Background(), TCPScanOptions{})
	if err != nil {
		t.Fatalf("ScanTCPTargets() error = %v", err)
	}
	mu.Lock()
	sort.Strings(addrs)
	mu.Unlock()

	want := []string{"127.0.0.1:5555", "127.0.0.1:5557", "127.0.0.1:5559", "127.0.0.1:5561", "127.0.0.1:5563", "127.0.0.1:5565", "127.0.0.1:5567", "127.0.0.1:5569", "127.0.0.1:5571", "127.0.0.1:5573", "127.0.0.1:5575", "127.0.0.1:5577", "127.0.0.1:5579", "127.0.0.1:5581", "127.0.0.1:5583", "127.0.0.1:5585"}
	if !reflect.DeepEqual(addrs, want) {
		t.Fatalf("scanned addrs = %#v, want %#v", addrs, want)
	}
}

func TestScanTCPTargetsCanIncludeEvenPorts(t *testing.T) {
	var mu sync.Mutex
	var addrs []string
	restore := replaceScanTCPConnect(func(ctx context.Context, addr string, opts ConnectOptions) (*Client, error) {
		mu.Lock()
		addrs = append(addrs, addr)
		mu.Unlock()
		return nil, errors.New("connection refused")
	})
	defer restore()

	_, err := ScanTCPTargets(context.Background(), TCPScanOptions{StartPort: 5554, EndPort: 5556, IncludeEvenPorts: true})
	if err != nil {
		t.Fatalf("ScanTCPTargets() error = %v", err)
	}
	mu.Lock()
	sort.Strings(addrs)
	mu.Unlock()

	want := []string{"127.0.0.1:5554", "127.0.0.1:5555", "127.0.0.1:5556"}
	if !reflect.DeepEqual(addrs, want) {
		t.Fatalf("scanned addrs = %#v, want %#v", addrs, want)
	}
}

func replaceScanTCPConnect(fn func(context.Context, string, ConnectOptions) (*Client, error)) func() {
	old := scanTCPConnect
	scanTCPConnect = fn
	return func() { scanTCPConnect = old }
}
