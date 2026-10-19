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
	"unicode/utf8"
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
	wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	// The terminal protocol sends small JSON envelopes (interactive output is
	// streamed in chunks), so accepting multi-megabyte frames only increases
	// the memory-amplification surface for a public WebSocket endpoint.
	maxPayloadLen     = 1 * 1024 * 1024 // 1 MiB maximum single frame
	maxJSONPayloadLen = 256 * 1024      // terminal protocol envelope limit
)

var (
	// ErrProtocolViolation indicates that the peer sent an invalid RFC 6455
	// frame. The connection is closed immediately after this error is returned.
	ErrProtocolViolation = errors.New("websocket protocol violation")
	ErrMessageTooLarge   = errors.New("websocket message exceeds maximum size")
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
	if r.Method != http.MethodGet {
		return nil, errors.New("websocket handshake must use GET")
	}
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return nil, errors.New("missing or invalid Upgrade header")
	}
	if !headerHasToken(r.Header.Get("Connection"), "upgrade") {
		return nil, errors.New("missing or invalid Connection header")
	}
	if strings.TrimSpace(r.Header.Get("Sec-WebSocket-Version")) != "13" {
		return nil, errors.New("unsupported WebSocket version")
	}
	key := strings.TrimSpace(r.Header.Get("Sec-WebSocket-Key"))
	if key == "" {
		return nil, errors.New("missing Sec-WebSocket-Key header")
	}
	decodedKey, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(decodedKey) != 16 {
		return nil, errors.New("invalid Sec-WebSocket-Key header")
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

func headerHasToken(value, wanted string) bool {
	for _, token := range strings.Split(value, ",") {
		if strings.EqualFold(strings.TrimSpace(token), wanted) {
			return true
		}
	}
	return false
}

// Dial connects to a WebSocket server at wsURL and performs the RFC 6455 handshake.
func Dial(ctx context.Context, wsURL string, header http.Header) (*Conn, error) {
	return dialWithContext(ctx, wsURL, header, nil)
}

// DialWithDialer is the context-aware WebSocket dial entry point used by
// monitors that need to pin hostname resolution (for example to prevent DNS
// rebinding/SSRF). A nil dialer uses the standard net.Dialer behavior.
func DialWithDialer(ctx context.Context, wsURL string, header http.Header, dialContext func(context.Context, string, string) (net.Conn, error)) (*Conn, error) {
	return dialWithContext(ctx, wsURL, header, dialContext)
}

func dialWithContext(ctx context.Context, wsURL string, header http.Header, dialContext func(context.Context, string, string) (net.Conn, error)) (*Conn, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	u, err := url.Parse(wsURL)
	if err != nil {
		return nil, fmt.Errorf("invalid websocket url: %w", err)
	}
	if u.User != nil || u.Hostname() == "" {
		return nil, errors.New("invalid websocket url: absolute host is required")
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "ws" && scheme != "wss" && scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("unsupported websocket scheme %q", u.Scheme)
	}
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		if scheme == "wss" || scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var dialer net.Dialer
	address := net.JoinHostPort(host, port)
	var netConn net.Conn
	if dialContext != nil {
		netConn, err = dialContext(ctx, "tcp", address)
	} else {
		netConn, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}
	// Keep cancellation effective during TLS and HTTP handshakes too. The
	// standard TLS helper that accepts a net.Dialer does not use DialContext,
	// so establish the TCP connection first and perform a context-aware
	// handshake explicitly.
	closeOnCancel := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = netConn.Close()
		case <-closeOnCancel:
		}
	}()
	defer close(closeOnCancel)

	if scheme == "wss" || scheme == "https" {
		tlsConn := tls.Client(netConn, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = netConn.Close()
			return nil, fmt.Errorf("tls handshake: %w", err)
		}
		netConn = tlsConn
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
		// These fields are part of the protocol handshake and must not be
		// overridden by caller-provided values. Apart from producing ambiguous
		// requests, duplicate Upgrade/Connection values can be interpreted
		// differently by intermediaries.
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "host", "upgrade", "connection", "sec-websocket-key", "sec-websocket-version":
			continue
		}
		if strings.ContainsAny(k, "\r\n") {
			netConn.Close()
			return nil, errors.New("invalid websocket header name")
		}
		for _, v := range vv {
			if strings.ContainsAny(v, "\r\n") {
				netConn.Close()
				return nil, errors.New("invalid websocket header value")
			}
			fmt.Fprintf(reqBuf, "%s: %s\r\n", k, v)
		}
	}
	fmt.Fprintf(reqBuf, "\r\n")

	if err := ctx.Err(); err != nil {
		_ = netConn.Close()
		return nil, err
	}
	if err := writeAll(netConn, reqBuf.Bytes()); err != nil {
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
	if !headerHasToken(resp.Header.Get("Upgrade"), "websocket") {
		netConn.Close()
		return nil, errors.New("websocket handshake response is missing Upgrade: websocket")
	}
	if !headerHasToken(resp.Header.Get("Connection"), "upgrade") {
		netConn.Close()
		return nil, errors.New("websocket handshake response is missing Connection: Upgrade")
	}
	if strings.TrimSpace(resp.Header.Get("Sec-WebSocket-Accept")) != expectedAccept {
		netConn.Close()
		return nil, errors.New("mismatched Sec-WebSocket-Accept header")
	}
	_ = netConn.SetDeadline(time.Time{})

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
		if header[0]&0x70 != 0 {
			return 0, nil, c.protocolError("reserved bits are set without an extension")
		}
		opcode := int(header[0] & 0x0F)
		masked := (header[1] & 0x80) != 0
		lengthCode := header[1] & 0x7F

		// RFC 6455 requires client-to-server frames to be masked and
		// server-to-client frames to be unmasked. Silently accepting the wrong
		// direction makes intermediary/proxy bugs and request smuggling easier.
		if c.isServer && !masked {
			return 0, nil, c.protocolError("client frame is not masked")
		}
		if !c.isServer && masked {
			return 0, nil, c.protocolError("server frame is masked")
		}

		if !validOpcode(opcode) {
			return 0, nil, c.protocolError("reserved or unsupported opcode")
		}
		if opcode == OpContinuation || !fin {
			// The terminal protocol intentionally does not implement fragmented
			// messages. Reject both a continuation and an unfinished data frame
			// instead of treating fragments as independent messages.
			return 0, nil, c.protocolError("fragmented messages are not supported")
		}

		var payloadLen uint64
		switch lengthCode {
		case 126:
			var ext [2]byte
			if _, err := io.ReadFull(c.br, ext[:]); err != nil {
				return 0, nil, err
			}
			payloadLen = uint64(binary.BigEndian.Uint16(ext[:]))
			if payloadLen < 126 {
				return 0, nil, c.protocolError("non-minimal 16-bit payload length")
			}
		case 127:
			var ext [8]byte
			if _, err := io.ReadFull(c.br, ext[:]); err != nil {
				return 0, nil, err
			}
			payloadLen = binary.BigEndian.Uint64(ext[:])
			if payloadLen&(uint64(1)<<63) != 0 {
				return 0, nil, c.protocolError("most-significant length bit is set")
			}
			if payloadLen < 65536 {
				return 0, nil, c.protocolError("non-minimal 64-bit payload length")
			}
		default:
			payloadLen = uint64(lengthCode)
		}

		isControl := opcode >= 0x8
		if isControl && (!fin || payloadLen > 125) {
			return 0, nil, c.protocolError("control frame must be final and at most 125 bytes")
		}
		if payloadLen > maxPayloadLen {
			_ = c.Close()
			return 0, nil, ErrMessageTooLarge
		}

		var maskKey [4]byte
		if masked {
			if _, err := io.ReadFull(c.br, maskKey[:]); err != nil {
				return 0, nil, err
			}
		}

		payload := make([]byte, int(payloadLen))
		if _, err := io.ReadFull(c.br, payload); err != nil {
			return 0, nil, err
		}

		if masked {
			for i := 0; i < len(payload); i++ {
				payload[i] ^= maskKey[i%4]
			}
		}

		switch opcode {
		case OpPing:
			if err := c.WriteMessage(OpPong, payload); err != nil {
				return 0, nil, err
			}
			continue
		case OpPong:
			continue
		case OpClose:
			if !validClosePayload(payload) {
				return 0, nil, c.protocolError("invalid close payload")
			}
			// Echo a valid close frame before closing the transport. This is
			// best-effort; the peer may already have gone away.
			_ = c.WriteMessage(OpClose, payload)
			_ = c.Close()
			return OpClose, payload, io.EOF
		case OpText, OpBinary:
			if opcode == OpText && !utf8.Valid(payload) {
				return 0, nil, c.protocolError("text frame is not valid UTF-8")
			}
			return opcode, payload, nil
		}
		// validOpcode above makes this unreachable, but keep a defensive error
		// if new opcodes are added without updating the parser.
		return 0, nil, c.protocolError("unsupported opcode")
	}
}

// WriteMessage sends a WebSocket frame.
func (c *Conn) WriteMessage(opcode int, payload []byte) error {
	if !validOpcode(opcode) || opcode == OpContinuation {
		return fmt.Errorf("%w: unsupported opcode %d", ErrProtocolViolation, opcode)
	}
	if opcode >= 0x8 && len(payload) > 125 {
		return fmt.Errorf("%w: control frame payload exceeds 125 bytes", ErrProtocolViolation)
	}
	if opcode == OpClose && !validClosePayload(payload) {
		return fmt.Errorf("%w: invalid close payload", ErrProtocolViolation)
	}
	if opcode == OpText && !utf8.Valid(payload) {
		return fmt.Errorf("%w: text frame is not valid UTF-8", ErrProtocolViolation)
	}
	if len(payload) > maxPayloadLen {
		return ErrMessageTooLarge
	}

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
		var ext [2]byte
		binary.BigEndian.PutUint16(ext[:], uint16(payloadLen))
		buf.Write(ext[:])
	} else {
		buf.WriteByte(maskBit | 127)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(payloadLen))
		buf.Write(ext[:])
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

	return writeAll(c.conn, buf.Bytes())
}

// WriteJSON encodes v as JSON and writes it as an OpText frame.
func (c *Conn) WriteJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(b) > maxJSONPayloadLen {
		return ErrMessageTooLarge
	}
	return c.WriteMessage(OpText, b)
}

// ReadJSON reads a message and unmarshals it into v.
func (c *Conn) ReadJSON(v any) error {
	op, b, err := c.ReadMessage()
	if err != nil {
		return err
	}
	if op != OpText {
		return fmt.Errorf("%w: JSON message must use a text frame", ErrProtocolViolation)
	}
	if len(b) > maxJSONPayloadLen {
		return ErrMessageTooLarge
	}
	return json.Unmarshal(b, v)
}

func validOpcode(opcode int) bool {
	switch opcode {
	case OpContinuation, OpText, OpBinary, OpClose, OpPing, OpPong:
		return true
	default:
		return false
	}
}

func validClosePayload(payload []byte) bool {
	if len(payload) == 1 {
		return false
	}
	if len(payload) == 0 {
		return true
	}
	code := binary.BigEndian.Uint16(payload[:2])
	validCode := (code >= 1000 && code <= 1003) ||
		(code >= 1007 && code <= 1014) ||
		(code >= 3000 && code <= 4999)
	if !validCode {
		return false
	}
	return len(payload) == 2 || utf8.Valid(payload[2:])
}

func (c *Conn) protocolError(reason string) error {
	// Closing the underlying transport immediately prevents a malformed peer
	// from keeping the read loop alive. A close control frame is intentionally
	// not written here: this path can run while the peer is no longer reading,
	// and a blocking close handshake would otherwise become a resource leak.
	_ = c.Close()
	return fmt.Errorf("%w: %s", ErrProtocolViolation, reason)
}

func writeAll(conn net.Conn, payload []byte) error {
	for len(payload) > 0 {
		n, err := conn.Write(payload)
		if n > 0 {
			payload = payload[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
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
