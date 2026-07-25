package protocol

import (
	"encoding/binary"
	"fmt"
	"io"
)

const messageHeaderLen = 24

// Command is an ADB protocol command identifier.
type Command uint32

const (
	// CommandSYNC identifies a SYNC packet.
	CommandSYNC Command = 'S' | 'Y'<<8 | 'N'<<16 | 'C'<<24
	// CommandCNXN identifies a CNXN packet.
	CommandCNXN Command = 'C' | 'N'<<8 | 'X'<<16 | 'N'<<24
	// CommandAUTH identifies an AUTH packet.
	CommandAUTH Command = 'A' | 'U'<<8 | 'T'<<16 | 'H'<<24
	// CommandOPEN identifies an OPEN packet.
	CommandOPEN Command = 'O' | 'P'<<8 | 'E'<<16 | 'N'<<24
	// CommandOKAY identifies an OKAY packet.
	CommandOKAY Command = 'O' | 'K'<<8 | 'A'<<16 | 'Y'<<24
	// CommandCLSE identifies a CLSE packet.
	CommandCLSE Command = 'C' | 'L'<<8 | 'S'<<16 | 'E'<<24
	// CommandWRTE identifies a WRTE packet.
	CommandWRTE Command = 'W' | 'R'<<8 | 'T'<<16 | 'E'<<24
)

// Message is a raw ADB protocol message.
type Message struct {
	Command Command
	Arg0    uint32
	Arg1    uint32
	Payload []byte
}

// Checksum returns the ADB payload checksum.
func Checksum(payload []byte) uint32 {
	var sum uint32
	for _, b := range payload {
		sum += uint32(b)
	}
	return sum
}

// Magic returns the command magic value expected by the ADB protocol.
func (c Command) Magic() uint32 {
	return uint32(c) ^ 0xffffffff
}

// ReadMessage reads and validates one ADB message from r.
func ReadMessage(r io.Reader) (Message, error) {
	var header [messageHeaderLen]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return Message{}, err
	}

	command := Command(binary.LittleEndian.Uint32(header[0:4]))
	arg0 := binary.LittleEndian.Uint32(header[4:8])
	arg1 := binary.LittleEndian.Uint32(header[8:12])
	payloadLen := binary.LittleEndian.Uint32(header[12:16])
	checksum := binary.LittleEndian.Uint32(header[16:20])
	magic := binary.LittleEndian.Uint32(header[20:24])

	if magic != command.Magic() {
		return Message{}, fmt.Errorf("adb message invalid command magic: command=%#x magic=%#x", uint32(command), magic)
	}

	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return Message{}, err
	}
	if got := Checksum(payload); got != checksum {
		return Message{}, fmt.Errorf("adb message invalid checksum: got %#x want %#x", got, checksum)
	}

	return Message{
		Command: command,
		Arg0:    arg0,
		Arg1:    arg1,
		Payload: payload,
	}, nil
}

// WriteMessage writes one ADB message to w.
func WriteMessage(w io.Writer, msg Message) error {
	var header [messageHeaderLen]byte
	binary.LittleEndian.PutUint32(header[0:4], uint32(msg.Command))
	binary.LittleEndian.PutUint32(header[4:8], msg.Arg0)
	binary.LittleEndian.PutUint32(header[8:12], msg.Arg1)
	binary.LittleEndian.PutUint32(header[12:16], uint32(len(msg.Payload)))
	binary.LittleEndian.PutUint32(header[16:20], Checksum(msg.Payload))
	binary.LittleEndian.PutUint32(header[20:24], msg.Command.Magic())

	if err := writeFull(w, header[:]); err != nil {
		return err
	}
	return writeFull(w, msg.Payload)
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
