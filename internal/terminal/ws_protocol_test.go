package terminal

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
)

func TestReadMessageRequiresCorrectMaskDirection(t *testing.T) {
	t.Run("server requires masked client frames", func(t *testing.T) {
		assertProtocolFrameError(t, true, testFrame(true, OpText, false, []byte("hello")))
	})
	t.Run("client rejects masked server frames", func(t *testing.T) {
		assertProtocolFrameError(t, false, testFrame(true, OpText, true, []byte("hello")))
	})
}

func TestReadMessageRejectsReservedBitsOpcodesAndFragments(t *testing.T) {
	cases := []struct {
		name  string
		frame []byte
	}{
		{name: "reserved bits", frame: testFrameWithFirstByte(0xC1, true, []byte("x"))},
		{name: "reserved opcode", frame: testFrame(true, 0x3, true, []byte("x"))},
		{name: "unfinished data frame", frame: testFrame(false, OpText, true, []byte("part"))},
		{name: "continuation frame", frame: testFrame(true, OpContinuation, true, []byte("part"))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertProtocolFrameError(t, true, tc.frame)
		})
	}
}

func TestReadMessageRejectsInvalidControlFrameLength(t *testing.T) {
	payload := make([]byte, 126)
	assertProtocolFrameError(t, true, testFrame(true, OpPing, true, payload))
}

func TestReadMessageRejectsOversizedFrame(t *testing.T) {
	// Send only the header and mask key: the parser must reject the declared
	// length before allocating or waiting for a megabyte-sized payload.
	frame := make([]byte, 2+8+4)
	frame[0] = 0x81
	frame[1] = 0xFF // masked, 64-bit extended length
	binary.BigEndian.PutUint64(frame[2:10], uint64(maxPayloadLen+1))
	copy(frame[10:], []byte{1, 2, 3, 4})
	assertMessageTooLarge(t, true, frame)
}

func TestWriteMessageUsesRequiredMaskDirection(t *testing.T) {
	t.Run("client masks", func(t *testing.T) {
		left, right := net.Pipe()
		defer left.Close()
		defer right.Close()
		client := &Conn{conn: left, isServer: false}

		writeErr := make(chan error, 1)
		go func() { writeErr <- client.WriteMessage(OpText, []byte("hello")) }()

		header := make([]byte, 2)
		if _, err := io.ReadFull(right, header); err != nil {
			t.Fatal(err)
		}
		if header[1]&0x80 == 0 {
			t.Fatal("client frame was not masked")
		}
		remaining := int(header[1] & 0x7F)
		if remaining != 5 {
			t.Fatalf("payload length = %d, want 5", remaining)
		}
		body := make([]byte, 4+remaining)
		if _, err := io.ReadFull(right, body); err != nil {
			t.Fatal(err)
		}
		if err := <-writeErr; err != nil {
			t.Fatal(err)
		}
	})

	t.Run("server does not mask", func(t *testing.T) {
		left, right := net.Pipe()
		defer left.Close()
		defer right.Close()
		server := &Conn{conn: left, isServer: true}

		writeErr := make(chan error, 1)
		go func() { writeErr <- server.WriteMessage(OpText, []byte("hello")) }()

		header := make([]byte, 2)
		if _, err := io.ReadFull(right, header); err != nil {
			t.Fatal(err)
		}
		if header[1]&0x80 != 0 {
			t.Fatal("server frame was unexpectedly masked")
		}
		body := make([]byte, int(header[1]&0x7F))
		if _, err := io.ReadFull(right, body); err != nil {
			t.Fatal(err)
		}
		if string(body) != "hello" {
			t.Fatalf("payload = %q, want hello", body)
		}
		if err := <-writeErr; err != nil {
			t.Fatal(err)
		}
	})
}

func TestWriteJSONRejectsOversizedEnvelope(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	conn := &Conn{conn: left, isServer: true}
	if err := conn.WriteJSON(map[string]string{"data": string(make([]byte, maxJSONPayloadLen))}); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("WriteJSON error = %v, want ErrMessageTooLarge", err)
	}
}

func assertProtocolFrameError(t *testing.T, isServer bool, frame []byte) {
	t.Helper()
	left, right := net.Pipe()
	conn := &Conn{conn: left, br: bufio.NewReader(left), isServer: isServer}
	defer conn.Close()

	go func() {
		_, _ = right.Write(frame)
		_ = right.Close()
	}()

	_, _, err := conn.ReadMessage()
	if !errors.Is(err, ErrProtocolViolation) {
		t.Fatalf("ReadMessage error = %v, want ErrProtocolViolation", err)
	}
	_ = right.Close()
}

func assertMessageTooLarge(t *testing.T, isServer bool, frame []byte) {
	t.Helper()
	left, right := net.Pipe()
	conn := &Conn{conn: left, br: bufio.NewReader(left), isServer: isServer}
	defer conn.Close()

	go func() {
		_, _ = right.Write(frame)
		_ = right.Close()
	}()

	_, _, err := conn.ReadMessage()
	if !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("ReadMessage error = %v, want ErrMessageTooLarge", err)
	}
	_ = right.Close()
}

func testFrame(fin bool, opcode int, masked bool, payload []byte) []byte {
	first := byte(opcode & 0x0F)
	if fin {
		first |= 0x80
	}
	return testFrameWithFirstByteAndPayload(first, masked, payload)
}

func testFrameWithFirstByte(first byte, fin bool, payload []byte) []byte {
	if fin {
		first |= 0x80
	}
	return testFrameWithFirstByteAndPayload(first, true, payload)
}

func testFrameWithFirstByteAndPayload(first byte, masked bool, payload []byte) []byte {
	var frame []byte
	maskBit := byte(0)
	if masked {
		maskBit = 0x80
	}
	switch {
	case len(payload) <= 125:
		frame = append(frame, first, maskBit|byte(len(payload)))
	case len(payload) <= 65535:
		frame = append(frame, first, maskBit|126, 0, 0)
		binary.BigEndian.PutUint16(frame[len(frame)-2:], uint16(len(payload)))
	default:
		frame = append(frame, first, maskBit|127, 0, 0, 0, 0, 0, 0, 0, 0)
		binary.BigEndian.PutUint64(frame[len(frame)-8:], uint64(len(payload)))
	}
	if masked {
		key := [4]byte{1, 2, 3, 4}
		frame = append(frame, key[:]...)
		for i, b := range payload {
			frame = append(frame, b^key[i%4])
		}
	} else {
		frame = append(frame, payload...)
	}
	return frame
}
