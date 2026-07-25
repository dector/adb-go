package protocol

import (
	"bytes"
	"encoding/binary"
	"io"
	"strings"
	"testing"
)

func TestMessageRoundTrip(t *testing.T) {
	in := Message{
		Command: CommandCNXN,
		Arg0:    0x01000000,
		Arg1:    4096,
		Payload: []byte("host::adb-go"),
	}

	var buf bytes.Buffer
	if err := WriteMessage(&buf, in); err != nil {
		t.Fatalf("WriteMessage() error = %v", err)
	}

	out, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("ReadMessage() error = %v", err)
	}
	if out.Command != in.Command || out.Arg0 != in.Arg0 || out.Arg1 != in.Arg1 || !bytes.Equal(out.Payload, in.Payload) {
		t.Fatalf("ReadMessage() = %#v, want %#v", out, in)
	}
}

func TestReadMessageInvalidChecksum(t *testing.T) {
	msg := Message{Command: CommandWRTE, Payload: []byte("hello")}

	var buf bytes.Buffer
	if err := WriteMessage(&buf, msg); err != nil {
		t.Fatalf("WriteMessage() error = %v", err)
	}
	raw := buf.Bytes()
	binary.LittleEndian.PutUint32(raw[16:20], Checksum(msg.Payload)+1)

	_, err := ReadMessage(bytes.NewReader(raw))
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("ReadMessage() error = %v, want checksum error", err)
	}
}

func TestReadMessageInvalidCommandMagic(t *testing.T) {
	msg := Message{Command: CommandOPEN, Payload: []byte("shell:id\x00")}

	var buf bytes.Buffer
	if err := WriteMessage(&buf, msg); err != nil {
		t.Fatalf("WriteMessage() error = %v", err)
	}
	raw := buf.Bytes()
	binary.LittleEndian.PutUint32(raw[20:24], msg.Command.Magic()+1)

	_, err := ReadMessage(bytes.NewReader(raw))
	if err == nil || !strings.Contains(err.Error(), "magic") {
		t.Fatalf("ReadMessage() error = %v, want magic error", err)
	}
}

func TestMessageEmptyPayload(t *testing.T) {
	in := Message{Command: CommandOKAY, Arg0: 1, Arg1: 2}

	var buf bytes.Buffer
	if err := WriteMessage(&buf, in); err != nil {
		t.Fatalf("WriteMessage() error = %v", err)
	}

	out, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("ReadMessage() error = %v", err)
	}
	if out.Command != in.Command || out.Arg0 != in.Arg0 || out.Arg1 != in.Arg1 || len(out.Payload) != 0 {
		t.Fatalf("ReadMessage() = %#v, want empty payload message %#v", out, in)
	}
}

func TestWriteMessageCompletesShortWrites(t *testing.T) {
	in := Message{Command: CommandWRTE, Arg0: 1, Arg1: 2, Payload: []byte("chunked payload")}
	var dst bytes.Buffer

	if err := WriteMessage(shortWriter{w: &dst, max: 3}, in); err != nil {
		t.Fatalf("WriteMessage() error = %v", err)
	}

	out, err := ReadMessage(&dst)
	if err != nil {
		t.Fatalf("ReadMessage() error = %v", err)
	}
	if out.Command != in.Command || out.Arg0 != in.Arg0 || out.Arg1 != in.Arg1 || !bytes.Equal(out.Payload, in.Payload) {
		t.Fatalf("ReadMessage() = %#v, want %#v", out, in)
	}
}

type shortWriter struct {
	w   io.Writer
	max int
}

func (w shortWriter) Write(p []byte) (int, error) {
	if len(p) > w.max {
		p = p[:w.max]
	}
	return w.w.Write(p)
}
