package protocol

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
)

// Stream is one logical ADB stream multiplexed over a Connection.
type Stream struct {
	conn *Connection

	localID  uint32
	remoteID uint32

	openOnce sync.Once
	openCh   chan error

	mu     sync.Mutex
	buf    []byte
	closed bool
	err    error
	dataCh chan []byte
}

// Open opens an ADB service stream using context.Background.
func (c *Connection) Open(service string) (*Stream, error) {
	return c.OpenContext(context.Background(), service)
}

// OpenContext opens an ADB service stream.
func (c *Connection) OpenContext(ctx context.Context, service string) (*Stream, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := c.startReader(); err != nil {
		return nil, err
	}

	stream := c.newStream()
	payload := []byte(strings.TrimSuffix(service, "\x00") + "\x00")
	if err := c.writeMessage(Message{Command: CommandOPEN, Arg0: stream.localID, Payload: payload}); err != nil {
		c.removeStream(stream.localID)
		return nil, fmt.Errorf("adb open service %q: %w", service, err)
	}

	select {
	case err := <-stream.openCh:
		if err != nil {
			return nil, fmt.Errorf("adb open service %q: %w", service, err)
		}
		return stream, nil
	case <-ctx.Done():
		_ = stream.Close()
		return nil, fmt.Errorf("adb open service %q canceled: %w", service, ctx.Err())
	}
}

// Read reads data from the stream. ADB WRTE packets are acknowledged with OKAY
// by the connection reader before their payload is made readable here.
func (s *Stream) Read(p []byte) (int, error) {
	for {
		s.mu.Lock()
		if len(s.buf) > 0 {
			n := copy(p, s.buf)
			s.buf = s.buf[n:]
			s.mu.Unlock()
			return n, nil
		}
		if s.closed {
			err := s.err
			s.mu.Unlock()
			select {
			case chunk, ok := <-s.dataCh:
				if ok {
					s.mu.Lock()
					s.buf = append(s.buf, chunk...)
					s.mu.Unlock()
					continue
				}
			default:
			}
			if err != nil {
				return 0, err
			}
			return 0, io.EOF
		}
		s.mu.Unlock()

		chunk, ok := <-s.dataCh
		if !ok {
			continue
		}
		s.mu.Lock()
		s.buf = append(s.buf, chunk...)
		s.mu.Unlock()
	}
}

// Write writes one ADB WRTE packet to the stream.
func (s *Stream) Write(p []byte) (int, error) {
	s.mu.Lock()
	closed := s.closed
	remoteID := s.remoteID
	s.mu.Unlock()
	if closed {
		return 0, io.ErrClosedPipe
	}
	if remoteID == 0 {
		return 0, io.ErrClosedPipe
	}

	payload := append([]byte(nil), p...)
	if err := s.conn.writeMessage(Message{Command: CommandWRTE, Arg0: s.localID, Arg1: remoteID, Payload: payload}); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Close closes the stream and notifies the peer with CLSE when possible.
func (s *Stream) Close() error {
	s.mu.Lock()
	remoteID := s.remoteID
	alreadyClosed := s.closed
	s.mu.Unlock()
	if !alreadyClosed && remoteID != 0 {
		_ = s.conn.writeMessage(Message{Command: CommandCLSE, Arg0: s.localID, Arg1: remoteID})
	}
	s.conn.closeStream(s.localID, io.ErrClosedPipe)
	return nil
}

func (s *Stream) establish(remoteID uint32) {
	s.mu.Lock()
	if s.remoteID == 0 {
		s.remoteID = remoteID
	}
	s.mu.Unlock()
	s.openOnce.Do(func() { s.openCh <- nil })
}

func (s *Stream) receive(payload []byte) {
	chunk := append([]byte(nil), payload...)
	s.dataCh <- chunk
}

func (s *Stream) closeWithError(err error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.err = err
	close(s.dataCh)
	s.mu.Unlock()
	s.openOnce.Do(func() { s.openCh <- err })
}

func (c *Connection) writeMessage(msg Message) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return WriteMessage(c.rw, msg)
}

func (c *Connection) startReader() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.readerStarted {
		return c.readerErr
	}
	if c.streams == nil {
		c.streams = make(map[uint32]*Stream)
	}
	c.readerStarted = true
	go c.readLoop()
	return nil
}

func (c *Connection) readLoop() {
	for {
		msg, err := ReadMessage(c.rw)
		if err != nil {
			c.mu.Lock()
			c.readerErr = err
			c.mu.Unlock()
			c.closeAllStreams(ErrDeviceClosed)
			return
		}
		c.handleStreamMessage(msg)
	}
}

func (c *Connection) handleStreamMessage(msg Message) {
	s := c.streamByLocalID(msg.Arg1)
	switch msg.Command {
	case CommandOKAY:
		if s != nil {
			s.establish(msg.Arg0)
		}
	case CommandWRTE:
		if s != nil {
			s.establish(msg.Arg0)
			_ = c.writeMessage(Message{Command: CommandOKAY, Arg0: s.localID, Arg1: msg.Arg0})
			s.receive(msg.Payload)
		}
	case CommandCLSE:
		if s != nil {
			s.mu.Lock()
			established := s.remoteID != 0
			s.mu.Unlock()
			if established {
				c.closeStream(s.localID, nil)
			} else {
				c.closeStream(s.localID, ErrDeviceClosed)
			}
		}
	}
}

func (c *Connection) newStream() *Stream {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.streams == nil {
		c.streams = make(map[uint32]*Stream)
	}
	c.nextLocalID++
	s := &Stream{
		conn:    c,
		localID: c.nextLocalID,
		openCh:  make(chan error, 1),
		dataCh:  make(chan []byte, 16),
	}
	c.streams[s.localID] = s
	return s
}

func (c *Connection) streamByLocalID(localID uint32) *Stream {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.streams[localID]
}

func (c *Connection) removeStream(localID uint32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.streams, localID)
}

func (c *Connection) closeStream(localID uint32, err error) {
	c.mu.Lock()
	s := c.streams[localID]
	delete(c.streams, localID)
	c.mu.Unlock()
	if s != nil {
		s.closeWithError(err)
	}
}

func (c *Connection) closeAllStreams(err error) {
	c.mu.Lock()
	streams := c.streams
	c.streams = make(map[uint32]*Stream)
	c.mu.Unlock()
	for _, s := range streams {
		s.closeWithError(err)
	}
}
