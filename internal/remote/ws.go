package remote

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	remoteWebSocketGUID               = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	remoteWebSocketOpcodeContinuation = 0x0
	remoteWebSocketOpcodeText         = 0x1
	remoteWebSocketOpcodeBinary       = 0x2
	remoteWebSocketOpcodeClose        = 0x8
	remoteWebSocketOpcodePing         = 0x9
	remoteWebSocketOpcodePong         = 0xA
	defaultRemoteWebSocketFrameLimit  = 10 * 1024 * 1024
	defaultRemoteWebSocketCloseCode   = 1000
)

type WebSocketOptions struct {
	WebSocketURL          string
	AuthToken             string
	Headers               map[string]string
	MaxFrames             int
	MaxFrameBytes         int64
	ReconnectAttempts     int
	ReconnectInitialDelay time.Duration
	ReconnectMaxDelay     time.Duration
	DialContext           func(context.Context, string, string) (net.Conn, error)
}

type WebSocketResult struct {
	CheckedAt      string      `json:"checked_at,omitempty"`
	Events         []PollEvent `json:"events,omitempty"`
	FrameCount     int         `json:"frame_count,omitempty"`
	ConnectCount   int         `json:"connect_count,omitempty"`
	ReconnectCount int         `json:"reconnect_count,omitempty"`
	CloseCode      int         `json:"close_code,omitempty"`
	LastError      string      `json:"last_error,omitempty"`
	Error          string      `json:"error,omitempty"`
}

type WebSocketEventHandler func([]PollEvent) error

type remoteWebSocketConn struct {
	net.Conn
	reader *bufio.Reader
}

type remoteWebSocketUpgradeError struct {
	StatusCode int
	RetryAfter string
}

func (e remoteWebSocketUpgradeError) Error() string {
	return fmt.Sprintf("remote websocket upgrade failed with status %d", e.StatusCode)
}

func FetchWebSocketEvents(ctx context.Context, options WebSocketOptions) WebSocketResult {
	result := WebSocketResult{CheckedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	rawURL := strings.TrimSpace(options.WebSocketURL)
	if rawURL == "" {
		result.Error = "remote websocket url is unavailable"
		return result
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "ws" && parsed.Scheme != "wss") {
		result.Error = fmt.Sprintf("invalid remote websocket url: %s", DisplayEndpoint(rawURL))
		return result
	}
	maxFrames := options.MaxFrames
	if maxFrames <= 0 {
		maxFrames = 1
	}
	frameLimit := options.MaxFrameBytes
	if frameLimit <= 0 {
		frameLimit = defaultRemoteWebSocketFrameLimit
	}
	for {
		conn, err := dialRemoteWebSocket(ctx, parsed, options)
		if err != nil {
			if ctx.Err() != nil {
				return result
			}
			result.LastError = err.Error()
			if !shouldReconnectRemoteWebSocket(ctx, options, result.ReconnectCount) {
				result.Error = result.LastError
				return result
			}
			if !backoffRemoteWebSocketAfter(ctx, options, result.ReconnectCount, err) {
				return result
			}
			result.ReconnectCount++
			continue
		}
		result.ConnectCount++
		transient, fatal := readRemoteWebSocketEvents(ctx, conn, frameLimit, maxFrames, &result)
		_ = conn.Close()
		if fatal != nil {
			result.Error = fatal.Error()
			return result
		}
		if result.FrameCount >= maxFrames || transient == nil {
			return result
		}
		result.LastError = transient.Error()
		if len(result.Events) > 0 {
			return result
		}
		if !shouldReconnectRemoteWebSocket(ctx, options, result.ReconnectCount) {
			result.Error = result.LastError
			return result
		}
		if !backoffRemoteWebSocket(ctx, options, result.ReconnectCount) {
			return result
		}
		result.ReconnectCount++
	}
}

func StreamWebSocketEvents(ctx context.Context, options WebSocketOptions, handler WebSocketEventHandler) WebSocketResult {
	result := WebSocketResult{CheckedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	rawURL := strings.TrimSpace(options.WebSocketURL)
	if rawURL == "" {
		result.Error = "remote websocket url is unavailable"
		return result
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "ws" && parsed.Scheme != "wss") {
		result.Error = fmt.Sprintf("invalid remote websocket url: %s", DisplayEndpoint(rawURL))
		return result
	}
	frameLimit := options.MaxFrameBytes
	if frameLimit <= 0 {
		frameLimit = defaultRemoteWebSocketFrameLimit
	}
	for {
		conn, err := dialRemoteWebSocket(ctx, parsed, options)
		if err != nil {
			if ctx.Err() != nil {
				return result
			}
			result.LastError = err.Error()
			if !shouldReconnectRemoteWebSocket(ctx, options, result.ReconnectCount) {
				result.Error = result.LastError
				return result
			}
			if !backoffRemoteWebSocketAfter(ctx, options, result.ReconnectCount, err) {
				return result
			}
			result.ReconnectCount++
			continue
		}
		result.ConnectCount++
		transient, fatal := streamRemoteWebSocketConnection(ctx, conn, frameLimit, options.MaxFrames, handler, &result)
		_ = conn.Close()
		if fatal != nil {
			result.Error = fatal.Error()
			return result
		}
		if ctx.Err() != nil || (options.MaxFrames > 0 && result.FrameCount >= options.MaxFrames) {
			return result
		}
		if transient == nil {
			return result
		}
		result.LastError = transient.Error()
		if !shouldReconnectRemoteWebSocket(ctx, options, result.ReconnectCount) {
			result.Error = result.LastError
			return result
		}
		if !backoffRemoteWebSocket(ctx, options, result.ReconnectCount) {
			return result
		}
		result.ReconnectCount++
	}
}

func readRemoteWebSocketEvents(ctx context.Context, conn *remoteWebSocketConn, frameLimit int64, maxFrames int, result *WebSocketResult) (error, error) {
	stopReadWatch := watchRemoteWebSocketReadContext(ctx, conn.Conn)
	defer stopReadWatch()
	for {
		opcode, payload, err := readRemoteWebSocketFrame(conn.reader, frameLimit)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, nil
			}
			if err == io.EOF {
				return nil, nil
			}
			return err, nil
		}
		switch opcode {
		case remoteWebSocketOpcodeText, remoteWebSocketOpcodeBinary:
			events, _, err := DecodePollEvents(payload)
			if err != nil {
				return nil, fmt.Errorf("decode remote websocket event: %v", err)
			}
			result.FrameCount++
			result.Events = append(result.Events, events...)
			if result.FrameCount >= maxFrames {
				return nil, nil
			}
		case remoteWebSocketOpcodePing:
			if err := writeRemoteWebSocketControlFrame(conn.Conn, remoteWebSocketOpcodePong, payload); err != nil {
				return err, nil
			}
		case remoteWebSocketOpcodeClose:
			result.CloseCode = remoteWebSocketCloseCode(payload)
			if result.CloseCode != defaultRemoteWebSocketCloseCode {
				return fmt.Errorf("remote websocket closed with code %d", result.CloseCode), nil
			}
			return nil, nil
		}
	}
}

func streamRemoteWebSocketConnection(ctx context.Context, conn *remoteWebSocketConn, frameLimit int64, maxFrames int, handler WebSocketEventHandler, result *WebSocketResult) (error, error) {
	stopReadWatch := watchRemoteWebSocketReadContext(ctx, conn.Conn)
	defer stopReadWatch()
	for {
		opcode, payload, err := readRemoteWebSocketFrame(conn.reader, frameLimit)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, nil
			}
			if err == io.EOF {
				return io.EOF, nil
			}
			return err, nil
		}
		switch opcode {
		case remoteWebSocketOpcodeText, remoteWebSocketOpcodeBinary:
			events, _, err := DecodePollEvents(payload)
			if err != nil {
				return nil, fmt.Errorf("decode remote websocket event: %v", err)
			}
			result.FrameCount++
			if len(events) > 0 {
				if handler != nil {
					if err := handler(events); err != nil {
						return nil, err
					}
				} else {
					result.Events = append(result.Events, events...)
				}
			}
			if maxFrames > 0 && result.FrameCount >= maxFrames {
				return nil, nil
			}
		case remoteWebSocketOpcodePing:
			if err := writeRemoteWebSocketControlFrame(conn.Conn, remoteWebSocketOpcodePong, payload); err != nil {
				return err, nil
			}
		case remoteWebSocketOpcodeClose:
			result.CloseCode = remoteWebSocketCloseCode(payload)
			if result.CloseCode != defaultRemoteWebSocketCloseCode {
				return fmt.Errorf("remote websocket closed with code %d", result.CloseCode), nil
			}
			return io.EOF, nil
		}
	}
}

func shouldReconnectRemoteWebSocket(ctx context.Context, options WebSocketOptions, reconnects int) bool {
	if ctx.Err() != nil {
		return false
	}
	if options.ReconnectAttempts < 0 {
		return true
	}
	return reconnects < options.ReconnectAttempts
}

func backoffRemoteWebSocket(ctx context.Context, options WebSocketOptions, reconnects int) bool {
	return backoffRemoteWebSocketAfter(ctx, options, reconnects, nil)
}

func backoffRemoteWebSocketAfter(ctx context.Context, options WebSocketOptions, reconnects int, err error) bool {
	delay := remoteWebSocketBackoffDelay(options, reconnects)
	if retryDelay, ok := remoteWebSocketRetryAfterDelay(err, time.Now()); ok {
		delay = retryDelay
		maxDelay := options.ReconnectMaxDelay
		if maxDelay <= 0 {
			maxDelay = 5 * time.Second
		}
		if delay > maxDelay {
			delay = maxDelay
		}
		if delay < 0 {
			delay = 0
		}
	}
	if delay <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func remoteWebSocketRetryAfterDelay(err error, now time.Time) (time.Duration, bool) {
	if err == nil {
		return 0, false
	}
	var upgradeErr remoteWebSocketUpgradeError
	if !errors.As(err, &upgradeErr) {
		return 0, false
	}
	return remoteRetryAfterDelay(upgradeErr.RetryAfter, now)
}

func remoteWebSocketBackoffDelay(options WebSocketOptions, reconnects int) time.Duration {
	delay := options.ReconnectInitialDelay
	if delay <= 0 {
		delay = 250 * time.Millisecond
	}
	for i := 0; i < reconnects; i++ {
		delay *= 2
	}
	maxDelay := options.ReconnectMaxDelay
	if maxDelay <= 0 {
		maxDelay = 5 * time.Second
	}
	if delay > maxDelay {
		return maxDelay
	}
	return delay
}

func dialRemoteWebSocket(ctx context.Context, parsed *url.URL, options WebSocketOptions) (*remoteWebSocketConn, error) {
	addr := remoteWebSocketDialAddress(parsed)
	dialContext := options.DialContext
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
	key, err := remoteWebSocketKey()
	if err != nil {
		_ = rawConn.Close()
		return nil, err
	}
	headers := make(map[string]string, len(options.Headers)+1)
	for key, value := range options.Headers {
		headers[key] = value
	}
	if strings.TrimSpace(options.AuthToken) != "" {
		headers["authorization"] = "Bearer " + strings.TrimSpace(options.AuthToken)
	}
	if err := writeRemoteWebSocketHandshake(rawConn, parsed, key, headers); err != nil {
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
		return nil, remoteWebSocketUpgradeError{StatusCode: response.StatusCode, RetryAfter: response.Header.Get("Retry-After")}
	}
	if !remoteHeaderContainsToken(response.Header.Get("Upgrade"), "websocket") || !remoteHeaderContainsToken(response.Header.Get("Connection"), "upgrade") {
		_ = rawConn.Close()
		return nil, fmt.Errorf("remote websocket upgrade response missing websocket headers")
	}
	if got, want := response.Header.Get("Sec-WebSocket-Accept"), remoteWebSocketAccept(key); got != want {
		_ = rawConn.Close()
		return nil, fmt.Errorf("remote websocket accept mismatch")
	}
	return &remoteWebSocketConn{Conn: rawConn, reader: reader}, nil
}

func watchRemoteWebSocketReadContext(ctx context.Context, conn net.Conn) func() {
	if ctx == nil || ctx.Done() == nil || conn == nil {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.SetReadDeadline(time.Now())
		case <-done:
		}
	}()
	return func() {
		close(done)
		_ = conn.SetReadDeadline(time.Time{})
	}
}

func writeRemoteWebSocketHandshake(w io.Writer, parsed *url.URL, key string, headers map[string]string) error {
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
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := headers[key]
		if !safeRemoteHeaderLine(key, value) {
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

func writeRemoteWebSocketFrame(w io.Writer, opcode byte, payload []byte) error {
	header, maskKey, err := remoteWebSocketFrameHeader(opcode, payload, true)
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

func writeRemoteWebSocketControlFrame(w io.Writer, opcode byte, payload []byte) error {
	if len(payload) > 125 {
		payload = payload[:125]
	}
	return writeRemoteWebSocketFrame(w, opcode, payload)
}

func readRemoteWebSocketFrame(r *bufio.Reader, limit int64) (byte, []byte, error) {
	var message []byte
	var messageOpcode byte
	for {
		opcode, fin, payload, err := readSingleRemoteWebSocketFrame(r, limit)
		if err != nil {
			return 0, nil, err
		}
		switch opcode {
		case remoteWebSocketOpcodeContinuation:
			if messageOpcode == 0 {
				return 0, nil, fmt.Errorf("remote websocket unexpected continuation frame")
			}
			message = append(message, payload...)
			if int64(len(message)) > limit {
				return 0, nil, fmt.Errorf("remote websocket frame exceeds %d bytes", limit)
			}
			if fin {
				return messageOpcode, message, nil
			}
		case remoteWebSocketOpcodeText, remoteWebSocketOpcodeBinary:
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

func readSingleRemoteWebSocketFrame(r *bufio.Reader, limit int64) (byte, bool, []byte, error) {
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
		return 0, false, nil, fmt.Errorf("remote websocket frame exceeds %d bytes", limit)
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

func remoteWebSocketFrameHeader(opcode byte, payload []byte, masked bool) ([]byte, [4]byte, error) {
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

func remoteWebSocketKey() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf[:]), nil
}

func remoteWebSocketAccept(key string) string {
	sum := sha1.Sum([]byte(key + remoteWebSocketGUID))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func remoteWebSocketDialAddress(parsed *url.URL) string {
	host := parsed.Host
	if strings.Contains(host, ":") {
		return host
	}
	if parsed.Scheme == "wss" {
		return net.JoinHostPort(host, "443")
	}
	return net.JoinHostPort(host, "80")
}

func remoteHeaderContainsToken(value string, token string) bool {
	for _, part := range strings.Split(value, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}

func safeRemoteHeaderLine(key string, value string) bool {
	return strings.TrimSpace(key) != "" && !strings.ContainsAny(key, "\r\n:") && !strings.ContainsAny(value, "\r\n")
}

func remoteWebSocketCloseCode(payload []byte) int {
	if len(payload) < 2 {
		return defaultRemoteWebSocketCloseCode
	}
	return int(binary.BigEndian.Uint16(payload[:2]))
}
