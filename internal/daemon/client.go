package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

func Send(ctx context.Context, socketPath string, req Request) (Response, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return Response{}, err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Response{}, fmt.Errorf("write daemon request: %w", err)
	}
	var resp Response
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&resp); err != nil {
		return Response{}, fmt.Errorf("read daemon response: %w", err)
	}
	return resp, nil
}

func Ping(ctx context.Context, socketPath string) (Response, error) {
	return Send(ctx, socketPath, Request{Version: ProtocolVersion, Command: CommandPing})
}

func probeContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 200*time.Millisecond)
}
