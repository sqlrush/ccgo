package mcp

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

const (
	webSocketGUID               = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	webSocketOpcodeContinuation = 0x0
	webSocketOpcodeText         = 0x1
	webSocketOpcodeBinary       = 0x2
	webSocketOpcodeClose        = 0x8
	webSocketOpcodePing         = 0x9
	webSocketOpcodePong         = 0xA
	DefaultWebSocketFrameLimit  = 10 * 1024 * 1024
	defaultWebSocketCloseCode   = 1000
)

type WSTransport struct {
	URL                   string
	Headers               map[string]string
	ProtocolVersionHeader string
	MaxFrameBytes         int64
	DialContext           func(context.Context, string, string) (net.Conn, error)

	mu   sync.Mutex
	conn *wsConn

	notificationMu      sync.RWMutex
	notificationHandler RPCNotificationHandler
	requestMu           sync.RWMutex
	requestHandler      RPCRequestHandler
}

type wsConn struct {
	net.Conn
	reader *bufio.Reader
}

func NewWSTransport(rawURL string, headers map[string]string) *WSTransport {
	return &WSTransport{
		URL:     strings.TrimSpace(rawURL),
		Headers: cloneStringMap(headers),
	}
}

func (t *WSTransport) RoundTrip(ctx context.Context, request RPCRequest) (RPCResponse, error) {
	if t == nil || t.URL == "" {
		return RPCResponse{}, fmt.Errorf("mcp ws transport url is required")
	}
	data, err := json.Marshal(request)
	if err != nil {
		return RPCResponse{}, err
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	conn, err := t.ensureConn(ctx)
	if err != nil {
		return RPCResponse{}, err
	}
	if err := writeWebSocketFrame(conn.Conn, webSocketOpcodeText, data); err != nil {
		_ = t.closeLocked()
		return RPCResponse{}, err
	}
	for {
		opcode, payload, err := readWebSocketFrame(conn.reader, t.frameLimit())
		if err != nil {
			_ = t.closeLocked()
			return RPCResponse{}, err
		}
		switch opcode {
		case webSocketOpcodeText, webSocketOpcodeBinary:
			var response RPCResponse
			if err := json.Unmarshal(payload, &response); err != nil {
				return RPCResponse{}, fmt.Errorf("decode mcp ws response: %w", err)
			}
			if err := t.handleInboundRequest(ctx, conn.Conn, response); err != nil {
				return RPCResponse{}, err
			}
			if _, ok := InboundRequestFromRPCResponse(response); ok {
				continue
			}
			if t.dispatchNotification(response) {
				continue
			}
			if response.ID == "" || response.ID != request.ID {
				continue
			}
			return response, nil
		case webSocketOpcodePing:
			if err := writeWebSocketControlFrame(conn.Conn, webSocketOpcodePong, payload); err != nil {
				_ = t.closeLocked()
				return RPCResponse{}, err
			}
		case webSocketOpcodeClose:
			_ = t.closeLocked()
			return RPCResponse{}, webSocketCloseError(payload)
		}
	}
}

func (t *WSTransport) SetRequestHandler(handler RPCRequestHandler) {
	if t == nil {
		return
	}
	t.requestMu.Lock()
	t.requestHandler = handler
	t.requestMu.Unlock()
}

func (t *WSTransport) handleInboundRequest(ctx context.Context, conn net.Conn, response RPCResponse) error {
	request, ok := InboundRequestFromRPCResponse(response)
	if !ok {
		return nil
	}
	t.requestMu.RLock()
	handler := t.requestHandler
	t.requestMu.RUnlock()
	data, err := json.Marshal(ResponseForInboundRequest(ctx, request, handler))
	if err != nil {
		return err
	}
	return writeWebSocketFrame(conn, webSocketOpcodeText, data)
}

func (t *WSTransport) SetNotificationHandler(handler RPCNotificationHandler) {
	if t == nil {
		return
	}
	t.notificationMu.Lock()
	t.notificationHandler = handler
	t.notificationMu.Unlock()
}

func (t *WSTransport) dispatchNotification(response RPCResponse) bool {
	notification, ok := NotificationFromRPCResponse(response)
	if !ok {
		return false
	}
	t.notificationMu.RLock()
	handler := t.notificationHandler
	t.notificationMu.RUnlock()
	if handler != nil {
		handler(notification)
	}
	return true
}

func (t *WSTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closeLocked()
}

func (t *WSTransport) ensureConn(ctx context.Context) (*wsConn, error) {
	if t.conn != nil {
		return t.conn, nil
	}
	conn, err := t.dial(ctx)
	if err != nil {
		return nil, err
	}
	t.conn = conn
	return conn, nil
}

func (t *WSTransport) closeLocked() error {
	if t.conn == nil {
		return nil
	}
	conn := t.conn
	t.conn = nil
	_ = writeWebSocketControlFrame(conn.Conn, webSocketOpcodeClose, webSocketClosePayload(defaultWebSocketCloseCode))
	return conn.Close()
}

func (t *WSTransport) dial(ctx context.Context) (*wsConn, error) {
	parsed, err := url.Parse(t.URL)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme != "ws" && parsed.Scheme != "wss" {
		return nil, fmt.Errorf("mcp ws transport requires ws or wss URL")
	}
	addr := webSocketDialAddress(parsed)
	dialContext := t.DialContext
	if dialContext == nil {
		dialer := &net.Dialer{}
		dialContext = dialer.DialContext
	}
	rawConn, err := dialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme == "wss" {
		tlsConn := tls.Client(rawConn, &tls.Config{ServerName: parsed.Hostname(), MinVersion: tls.VersionTLS12})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = rawConn.Close()
			return nil, err
		}
		rawConn = tlsConn
	}

	key, err := webSocketKey()
	if err != nil {
		_ = rawConn.Close()
		return nil, err
	}
	if err := writeWebSocketHandshake(rawConn, parsed, key, t.Headers, t.ProtocolVersionHeader); err != nil {
		_ = rawConn.Close()
		return nil, err
	}
	reader := bufio.NewReader(rawConn)
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
	if err != nil {
		_ = rawConn.Close()
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusSwitchingProtocols {
		_ = rawConn.Close()
		return nil, fmt.Errorf("mcp ws status %d", response.StatusCode)
	}
	if !headerContainsToken(response.Header.Get("Upgrade"), "websocket") || !headerContainsToken(response.Header.Get("Connection"), "upgrade") {
		_ = rawConn.Close()
		return nil, fmt.Errorf("mcp ws upgrade response missing websocket headers")
	}
	if got, want := response.Header.Get("Sec-WebSocket-Accept"), webSocketAccept(key); got != want {
		_ = rawConn.Close()
		return nil, fmt.Errorf("mcp ws accept mismatch")
	}
	return &wsConn{Conn: rawConn, reader: reader}, nil
}

func (t *WSTransport) frameLimit() int64 {
	if t.MaxFrameBytes > 0 {
		return t.MaxFrameBytes
	}
	return DefaultWebSocketFrameLimit
}

func writeWebSocketHandshake(w io.Writer, parsed *url.URL, key string, headers map[string]string, protocolVersion string) error {
	path := parsed.EscapedPath()
	if path == "" {
		path = "/"
	}
	if parsed.RawQuery != "" {
		path += "?" + parsed.RawQuery
	}
	var b strings.Builder
	b.WriteString("GET ")
	b.WriteString(path)
	b.WriteString(" HTTP/1.1\r\n")
	b.WriteString("Host: ")
	b.WriteString(parsed.Host)
	b.WriteString("\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: ")
	b.WriteString(key)
	b.WriteString("\r\n")
	if protocolVersion != "" {
		b.WriteString("mcp-protocol-version: ")
		b.WriteString(protocolVersion)
		b.WriteString("\r\n")
	}
	for _, key := range sortedHeaderKeys(headers) {
		value := headers[key]
		if !safeHeaderLine(key, value) {
			continue
		}
		b.WriteString(key)
		b.WriteString(": ")
		b.WriteString(value)
		b.WriteString("\r\n")
	}
	b.WriteString("\r\n")
	_, err := io.WriteString(w, b.String())
	return err
}

func writeWebSocketFrame(w io.Writer, opcode byte, payload []byte) error {
	header, maskKey, err := webSocketFrameHeader(opcode, payload, true)
	if err != nil {
		return err
	}
	masked := append([]byte(nil), payload...)
	for i := range masked {
		masked[i] ^= maskKey[i%4]
	}
	if _, err := w.Write(header); err != nil {
		return err
	}
	_, err = w.Write(masked)
	return err
}

func writeWebSocketControlFrame(w io.Writer, opcode byte, payload []byte) error {
	if len(payload) > 125 {
		payload = payload[:125]
	}
	return writeWebSocketFrame(w, opcode, payload)
}

func readWebSocketFrame(r *bufio.Reader, limit int64) (byte, []byte, error) {
	var message []byte
	var messageOpcode byte
	for {
		opcode, fin, payload, err := readSingleWebSocketFrame(r, limit)
		if err != nil {
			return 0, nil, err
		}
		switch opcode {
		case webSocketOpcodeContinuation:
			if messageOpcode == 0 {
				return 0, nil, fmt.Errorf("mcp ws unexpected continuation frame")
			}
			message = append(message, payload...)
			if int64(len(message)) > limit {
				return 0, nil, fmt.Errorf("mcp ws frame exceeds %d bytes", limit)
			}
			if fin {
				return messageOpcode, message, nil
			}
		case webSocketOpcodeText, webSocketOpcodeBinary:
			if fin {
				return opcode, payload, nil
			}
			messageOpcode = opcode
			message = append(message[:0], payload...)
		default:
			return opcode, payload, nil
		}
	}
}

func readSingleWebSocketFrame(r *bufio.Reader, limit int64) (byte, bool, []byte, error) {
	first, err := r.ReadByte()
	if err != nil {
		return 0, false, nil, err
	}
	second, err := r.ReadByte()
	if err != nil {
		return 0, false, nil, err
	}
	fin := first&0x80 != 0
	opcode := first & 0x0f
	masked := second&0x80 != 0
	length := int64(second & 0x7f)
	switch length {
	case 126:
		var buf [2]byte
		if _, err := io.ReadFull(r, buf[:]); err != nil {
			return 0, false, nil, err
		}
		length = int64(binary.BigEndian.Uint16(buf[:]))
	case 127:
		var buf [8]byte
		if _, err := io.ReadFull(r, buf[:]); err != nil {
			return 0, false, nil, err
		}
		length = int64(binary.BigEndian.Uint64(buf[:]))
	}
	if length > limit {
		return 0, false, nil, fmt.Errorf("mcp ws frame exceeds %d bytes", limit)
	}
	var maskKey [4]byte
	if masked {
		if _, err := io.ReadFull(r, maskKey[:]); err != nil {
			return 0, false, nil, err
		}
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, false, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= maskKey[i%4]
		}
	}
	return opcode, fin, payload, nil
}

func webSocketFrameHeader(opcode byte, payload []byte, masked bool) ([]byte, [4]byte, error) {
	var maskKey [4]byte
	if masked {
		if _, err := rand.Read(maskKey[:]); err != nil {
			return nil, maskKey, err
		}
	}
	header := []byte{0x80 | opcode}
	maskBit := byte(0)
	if masked {
		maskBit = 0x80
	}
	length := len(payload)
	switch {
	case length <= 125:
		header = append(header, maskBit|byte(length))
	case length <= 65535:
		header = append(header, maskBit|126)
		var buf [2]byte
		binary.BigEndian.PutUint16(buf[:], uint16(length))
		header = append(header, buf[:]...)
	default:
		header = append(header, maskBit|127)
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], uint64(length))
		header = append(header, buf[:]...)
	}
	if masked {
		header = append(header, maskKey[:]...)
	}
	return header, maskKey, nil
}

func webSocketKey() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf[:]), nil
}

func webSocketAccept(key string) string {
	sum := sha1.Sum([]byte(key + webSocketGUID))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func webSocketDialAddress(parsed *url.URL) string {
	host := parsed.Host
	if strings.Contains(host, ":") {
		return host
	}
	if parsed.Scheme == "wss" {
		return net.JoinHostPort(host, "443")
	}
	return net.JoinHostPort(host, "80")
}

func headerContainsToken(value string, token string) bool {
	for _, part := range strings.Split(value, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}

func safeHeaderLine(key string, value string) bool {
	return strings.TrimSpace(key) != "" && !strings.ContainsAny(key, "\r\n:") && !strings.ContainsAny(value, "\r\n")
}

func sortedHeaderKeys(headers map[string]string) []string {
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sortStrings(keys)
	return keys
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

func webSocketClosePayload(code int) []byte {
	var buf [2]byte
	binary.BigEndian.PutUint16(buf[:], uint16(code))
	return buf[:]
}

func webSocketCloseCode(payload []byte) int {
	if len(payload) < 2 {
		return defaultWebSocketCloseCode
	}
	return int(binary.BigEndian.Uint16(payload[:2]))
}

func webSocketCloseError(payload []byte) error {
	code := webSocketCloseCode(payload)
	if code == defaultWebSocketCloseCode {
		return io.EOF
	}
	return fmt.Errorf("mcp ws closed with code %d", code)
}
