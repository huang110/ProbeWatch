package terminal

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// RFC 6455 Frame Opcodes
const (
	OpContinuation = 0x0
	OpText         = 0x1
	OpBinary       = 0x2
	OpClose        = 0x8
	OpPing         = 0x9
	OpPong         = 0xA
)

const (
	wsGUID        = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	maxPayloadLen = 16 * 1024 * 1024 // 16MB max message
)

// Conn represents an RFC 6455 WebSocket connection.
type Conn struct {
	conn     net.Conn
	br       *bufio.Reader
	isServer bool
	readMu   sync.Mutex
	writeMu  sync.Mutex
	closed   bool
	closeMu  sync.Mutex
}

// Upgrade upgrades an incoming HTTP request to an RFC 6455 WebSocket connection.
func Upgrade(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return nil, errors.New("missing or invalid Upgrade header")
	}
	key := strings.TrimSpace(r.Header.Get("Sec-WebSocket-Key"))
	if key == "" {
		return nil, errors.New("missing Sec-WebSocket-Key header")
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("server does not support hijacking")
	}

	h := sha1.New()
	h.Write([]byte(key + wsGUID))
	acceptKey := base64.StdEncoding.EncodeToString(h.Sum(nil))

	netConn, brw, err := hijacker.Hijack()
	if err != nil {
		return nil, fmt.Errorf("hijack connection: %w", err)
	}

	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + acceptKey + "\r\n\r\n"

	if _, err := brw.WriteString(resp); err != nil {
		netConn.Close()
		return nil, fmt.Errorf("write upgrade response: %w", err)
	}
	if err := brw.Flush(); err != nil {
		netConn.Close()
		return nil, fmt.Errorf("flush upgrade response: %w", err)
	}

	return &Conn{
		conn:     netConn,
		br:       brw.Reader,
		isServer: true,
	}, nil
}

// Dial connects to a WebSocket server at wsURL and performs the RFC 6455 handshake.
func Dial(ctx context.Context, wsURL string, header http.Header) (*Conn, error) {
	u, err := url.Parse(wsURL)
	if err != nil {
		return nil, fmt.Errorf("invalid websocket url: %w", err)
	}

	var host, port string
	scheme := strings.ToLower(u.Scheme)
	if strings.Contains(u.Host, ":") {
		host, port, err = net.SplitHostPort(u.Host)
		if err != nil {
			return nil, err
		}
	} else {
		host = u.Host
		if scheme == "wss" || scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}

	var netConn net.Conn
	var dialer net.Dialer
	if scheme == "wss" || scheme == "https" {
		tlsConfig := &tls.Config{ServerName: host}
		netConn, err = tls.DialWithDialer(&dialer, "tcp", net.JoinHostPort(host, port), tlsConfig)
	} else {
		netConn, err = dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	}
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}

	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		netConn.Close()
		return nil, err
	}
	key := base64.StdEncoding.EncodeToString(nonce)

	h := sha1.New()
	h.Write([]byte(key + wsGUID))
	expectedAccept := base64.StdEncoding.EncodeToString(h.Sum(nil))

	reqPath := u.Path
	if reqPath == "" {
		reqPath = "/"
	}
	if u.RawQuery != "" {
		reqPath += "?" + u.RawQuery
	}

	reqBuf := bytes.NewBuffer(nil)
	fmt.Fprintf(reqBuf, "GET %s HTTP/1.1\r\n", reqPath)
	fmt.Fprintf(reqBuf, "Host: %s\r\n", u.Host)
	fmt.Fprintf(reqBuf, "Upgrade: websocket\r\n")
	fmt.Fprintf(reqBuf, "Connection: Upgrade\r\n")
	fmt.Fprintf(reqBuf, "Sec-WebSocket-Key: %s\r\n", key)
	fmt.Fprintf(reqBuf, "Sec-WebSocket-Version: 13\r\n")
	for k, vv := range header {
		for _, v := range vv {
			fmt.Fprintf(reqBuf, "%s: %s\r\n", k, v)
		}
	}
	fmt.Fprintf(reqBuf, "\r\n")

	if _, err := netConn.Write(reqBuf.Bytes()); err != nil {
		netConn.Close()
		return nil, fmt.Errorf("write handshake request: %w", err)
	}

	br := bufio.NewReader(netConn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		netConn.Close()
		return nil, fmt.Errorf("read handshake response: %w", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		netConn.Close()
		return nil, fmt.Errorf("websocket handshake failed with status %d", resp.StatusCode)
	}
	if strings.TrimSpace(resp.Header.Get("Sec-WebSocket-Accept")) != expectedAccept {
		netConn.Close()
		return nil, errors.New("mismatched Sec-WebSocket-Accept header")
	}

	return &Conn{
		conn:     netConn,
		br:       br,
		isServer: false,
	}, nil
}

// ReadMessage reads the next data frame from the connection.
// Automatically responds to Pings with Pongs.
func (c *Conn) ReadMessage() (int, []byte, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()

	for {
		header := make([]byte, 2)
		if _, err := io.ReadFull(c.br, header); err != nil {
			return 0, nil, err
		}

		fin := (header[0] & 0x80) != 0
		opcode := int(header[0] & 0x0F)
		masked := (header[1] & 0x80) != 0
		payloadLen := int(header[1] & 0x7F)

		if !fin && opcode != OpContinuation {
			// Fragments not supported for simplicity
		}

		if payloadLen == 126 {
			var extLen uint16
			if err := binary.Read(c.br, binary.BigEndian, &extLen); err != nil {
				return 0, nil, err
			}
			payloadLen = int(extLen)
		} else if payloadLen == 127 {
			var extLen uint64
			if err := binary.Read(c.br, binary.BigEndian, &extLen); err != nil {
				return 0, nil, err
			}
			if extLen > maxPayloadLen {
				return 0, nil, errors.New("frame payload exceeds maximum allowed size")
			}
			payloadLen = int(extLen)
		}

		var maskKey [4]byte
		if masked {
			if _, err := io.ReadFull(c.br, maskKey[:]); err != nil {
				return 0, nil, err
			}
		}

		payload := make([]byte, payloadLen)
		if _, err := io.ReadFull(c.br, payload); err != nil {
			return 0, nil, err
		}

		if masked {
			for i := 0; i < payloadLen; i++ {
				payload[i] ^= maskKey[i%4]
			}
		}

		switch opcode {
		case OpPing:
			_ = c.WriteMessage(OpPong, payload)
			continue
		case OpPong:
			continue
		case OpClose:
			_ = c.Close()
			return OpClose, payload, io.EOF
		case OpText, OpBinary:
			return opcode, payload, nil
		default:
			// Ignore unknown control frames
			continue
		}
	}
}

// WriteMessage sends a WebSocket frame.
func (c *Conn) WriteMessage(opcode int, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	c.closeMu.Lock()
	if c.closed {
		c.closeMu.Unlock()
		return errors.New("connection closed")
	}
	c.closeMu.Unlock()

	var buf bytes.Buffer
	b0 := byte(0x80 | (opcode & 0x0F)) // FIN = 1
	buf.WriteByte(b0)

	payloadLen := len(payload)
	maskBit := byte(0)
	if !c.isServer {
		maskBit = 0x80 // client must mask frames
	}

	if payloadLen <= 125 {
		buf.WriteByte(maskBit | byte(payloadLen))
	} else if payloadLen <= 65535 {
		buf.WriteByte(maskBit | 126)
		_ = binary.Write(&buf, binary.BigEndian, uint16(payloadLen))
	} else {
		buf.WriteByte(maskBit | 127)
		_ = binary.Write(&buf, binary.BigEndian, uint64(payloadLen))
	}

	if !c.isServer {
		var maskKey [4]byte
		if _, err := rand.Read(maskKey[:]); err != nil {
			return err
		}
		buf.Write(maskKey[:])

		masked := make([]byte, payloadLen)
		for i := 0; i < payloadLen; i++ {
			masked[i] = payload[i] ^ maskKey[i%4]
		}
		buf.Write(masked)
	} else {
		buf.Write(payload)
	}

	_, err := c.conn.Write(buf.Bytes())
	return err
}

// WriteJSON encodes v as JSON and writes it as an OpText frame.
func (c *Conn) WriteJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.WriteMessage(OpText, b)
}

// ReadJSON reads a message and unmarshals it into v.
func (c *Conn) ReadJSON(v any) error {
	_, b, err := c.ReadMessage()
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// Close gracefully closes the underlying connection.
func (c *Conn) Close() error {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	return c.conn.Close()
}

func (c *Conn) SetDeadline(t time.Time) error      { return c.conn.SetDeadline(t) }
func (c *Conn) SetReadDeadline(t time.Time) error  { return c.conn.SetReadDeadline(t) }
func (c *Conn) SetWriteDeadline(t time.Time) error { return c.conn.SetWriteDeadline(t) }
