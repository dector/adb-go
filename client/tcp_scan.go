package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/dector/adb-go/protocol"
)

const (
	defaultTCPScanHost      = "127.0.0.1"
	defaultTCPScanStartPort = 5555
	defaultTCPScanEndPort   = 5585
	defaultTCPScanTimeout   = 300 * time.Millisecond
)

// TCPTarget describes one TCP ADB connection target discovered by scanning.
type TCPTarget struct {
	// Addr is the normalized host:port selector that can be passed to ConnectTCP
	// or to the CLI's --addr flag.
	Addr string

	// Host and Port are the scan candidate that produced Addr.
	Host string
	Port int

	// AuthRequired is true when the endpoint completed enough of the ADB
	// handshake to request authentication, but no supplied credential completed
	// authentication.
	AuthRequired bool
}

// TCPScanOptions controls TCP ADB target scanning.
type TCPScanOptions struct {
	// Host is the host to scan. The default is 127.0.0.1.
	Host string

	// StartPort and EndPort are inclusive. The defaults are 5555 and 5585.
	StartPort int
	EndPort   int

	// IncludeEvenPorts scans both even and odd ports when true. By default only
	// odd ports are scanned because Android emulators use even console ports and
	// odd ADB ports.
	IncludeEvenPorts bool

	// Timeout is the per-port connection and ADB handshake timeout. The default is
	// 300ms.
	Timeout time.Duration

	// AuthCredentials contains explicit ADB host credentials used when a TCP
	// target requires authentication.
	AuthCredentials []protocol.AuthCredential
}

var scanTCPConnect = ConnectTCPWithOptions

// ScanTCPTargets scans a small TCP port range for ADB endpoints. By default it
// scans localhost odd emulator ADB ports 5555..5585 with a short per-port
// timeout. Targets are returned when the ADB handshake succeeds or when the
// endpoint requests authentication.
func ScanTCPTargets(ctx context.Context, opts TCPScanOptions) ([]TCPTarget, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	opts = defaultTCPScanOptions(opts)
	if opts.StartPort < 1 || opts.StartPort > 65535 {
		return nil, fmt.Errorf("adb tcp scan start port %d out of range", opts.StartPort)
	}
	if opts.EndPort < 1 || opts.EndPort > 65535 {
		return nil, fmt.Errorf("adb tcp scan end port %d out of range", opts.EndPort)
	}
	if opts.StartPort > opts.EndPort {
		return nil, fmt.Errorf("adb tcp scan start port %d is greater than end port %d", opts.StartPort, opts.EndPort)
	}
	if opts.Timeout <= 0 {
		return nil, fmt.Errorf("adb tcp scan timeout must be positive")
	}

	ports := tcpScanPorts(opts)
	results := make(chan TCPTarget, len(ports))
	var wg sync.WaitGroup
	for _, port := range ports {
		port := port
		wg.Add(1)
		go func() {
			defer wg.Done()
			if target, ok := scanTCPPort(ctx, opts, port); ok {
				results <- target
			}
		}()
	}
	wg.Wait()
	close(results)

	targets := make([]TCPTarget, 0, len(results))
	for target := range results {
		targets = append(targets, target)
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].Host != targets[j].Host {
			return targets[i].Host < targets[j].Host
		}
		return targets[i].Port < targets[j].Port
	})
	return targets, nil
}

func defaultTCPScanOptions(opts TCPScanOptions) TCPScanOptions {
	if opts.Host == "" {
		opts.Host = defaultTCPScanHost
	}
	if opts.StartPort == 0 {
		opts.StartPort = defaultTCPScanStartPort
	}
	if opts.EndPort == 0 {
		opts.EndPort = defaultTCPScanEndPort
	}
	if opts.Timeout == 0 {
		opts.Timeout = defaultTCPScanTimeout
	}
	return opts
}

func tcpScanPorts(opts TCPScanOptions) []int {
	ports := make([]int, 0, opts.EndPort-opts.StartPort+1)
	for port := opts.StartPort; port <= opts.EndPort; port++ {
		if !opts.IncludeEvenPorts && port%2 == 0 {
			continue
		}
		ports = append(ports, port)
	}
	return ports
}

func scanTCPPort(ctx context.Context, opts TCPScanOptions, port int) (TCPTarget, bool) {
	addr := net.JoinHostPort(opts.Host, strconv.Itoa(port))
	portCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	client, err := scanTCPConnect(portCtx, addr, ConnectOptions{AuthCredentials: opts.AuthCredentials})
	if err == nil {
		if client != nil {
			_ = client.Close()
		}
		return TCPTarget{Addr: addr, Host: opts.Host, Port: port}, true
	}
	if errors.Is(err, ErrAuthRequired) {
		return TCPTarget{Addr: addr, Host: opts.Host, Port: port, AuthRequired: true}, true
	}
	return TCPTarget{}, false
}
