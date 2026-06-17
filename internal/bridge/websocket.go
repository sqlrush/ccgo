package bridge

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"

	"ccgo/internal/contracts"
)

const (
	webSocketMagicGUID        = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	webSocketFrameLimit       = 64 * 1024
	webSocketProtocolVersion  = 1
	webSocketActionHello      = "hello"
	webSocketActionHealth     = "health"
	webSocketActionManifest   = "manifest"
	webSocketActionResolve    = "resolve"
	webSocketActionExecute    = "execute"
	webSocketActionRemoteTrig = "remote_trigger"
)

type DirectWebSocketRequest struct {
	Action        string                      `json:"action,omitempty"`
	Command       string                      `json:"command,omitempty"`
	UUID          contracts.ID                `json:"uuid,omitempty"`
	RemoteTrigger *DirectRemoteTriggerRequest `json:"remote_trigger,omitempty"`
}

type DirectWebSocketResponse struct {
	Type          string                       `json:"type"`
	Hello         *DirectWebSocketHello        `json:"hello,omitempty"`
	Health        *DirectHealthResponse        `json:"health,omitempty"`
	Manifest      *Manifest                    `json:"manifest,omitempty"`
	Resolve       *DirectResolveResponse       `json:"resolve,omitempty"`
	Execute       *DirectExecuteResponse       `json:"execute,omitempty"`
	RemoteTrigger *DirectRemoteTriggerResponse `json:"remote_trigger,omitempty"`
	Error         string                       `json:"error,omitempty"`
}

type DirectWebSocketHello struct {
	OK                  bool         `json:"ok"`
	ProtocolVersion     int          `json:"protocol_version"`
	SessionID           contracts.ID `json:"session_id,omitempty"`
	Commands            int          `json:"commands"`
	Capabilities        []Capability `json:"capabilities,omitempty"`
	Actions             []string     `json:"actions"`
	ManifestGeneratedAt string       `json:"manifest_generated_at,omitempty"`
}

func (h *DirectHandler) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeDirectError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !headerContainsToken(r.Header.Get("Connection"), "upgrade") || !strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket") {
		writeDirectError(w, http.StatusBadRequest, "websocket upgrade is required")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Sec-WebSocket-Key"))
	if key == "" || strings.TrimSpace(r.Header.Get("Sec-WebSocket-Version")) != "13" {
		writeDirectError(w, http.StatusBadRequest, "invalid websocket handshake")
		return
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		writeDirectError(w, http.StatusInternalServerError, "websocket hijack is unavailable")
		return
	}
	conn, rw, err := hijacker.Hijack()
	if err != nil {
		return
	}
	defer conn.Close()
	if _, err := fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", webSocketAccept(key)); err != nil {
		return
	}
	if err := rw.Flush(); err != nil {
		return
	}
	h.serveWebSocket(conn, rw.Reader)
}

func (h *DirectHandler) serveWebSocket(conn net.Conn, reader *bufio.Reader) {
	for {
		opcode, payload, err := readWebSocketFrame(reader)
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			_ = writeWebSocketJSON(conn, DirectWebSocketResponse{Type: "error", Error: err.Error()})
			return
		}
		switch opcode {
		case 0x1:
			if err := writeWebSocketJSON(conn, h.handleWebSocketMessage(payload)); err != nil {
				return
			}
		case 0x8:
			_ = writeWebSocketFrame(conn, 0x8, nil)
			return
		case 0x9:
			if err := writeWebSocketFrame(conn, 0xA, payload); err != nil {
				return
			}
		default:
			_ = writeWebSocketJSON(conn, DirectWebSocketResponse{Type: "error", Error: "unsupported websocket frame opcode"})
			return
		}
	}
}

func (h *DirectHandler) handleWebSocketMessage(payload []byte) DirectWebSocketResponse {
	var req DirectWebSocketRequest
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return DirectWebSocketResponse{Type: "error", Error: "invalid JSON request: " + err.Error()}
	}
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action == "" {
		action = webSocketActionExecute
	}
	switch action {
	case webSocketActionHello, "status":
		hello := h.webSocketHello()
		return DirectWebSocketResponse{Type: webSocketActionHello, Hello: &hello}
	case webSocketActionHealth:
		health := h.health()
		return DirectWebSocketResponse{Type: webSocketActionHealth, Health: &health}
	case webSocketActionManifest:
		manifest := h.manifest
		return DirectWebSocketResponse{Type: webSocketActionManifest, Manifest: &manifest}
	case webSocketActionResolve:
		resolved := h.resolve(req.Command)
		return DirectWebSocketResponse{Type: webSocketActionResolve, Resolve: &resolved}
	case webSocketActionExecute:
		executed, _ := h.execute(DirectCommandRequest{Command: req.Command, UUID: req.UUID})
		return DirectWebSocketResponse{Type: webSocketActionExecute, Execute: &executed}
	case webSocketActionRemoteTrig, "remote-trigger":
		if h.remoteTrigger == nil {
			return DirectWebSocketResponse{Type: "error", Error: "remote trigger endpoint is not configured"}
		}
		if req.RemoteTrigger == nil {
			return DirectWebSocketResponse{Type: "error", Error: "remote_trigger is required"}
		}
		remoteTrigger, err := normalizeDirectRemoteTriggerRequest(*req.RemoteTrigger)
		if err != nil {
			return DirectWebSocketResponse{Type: "error", Error: err.Error()}
		}
		response, _ := h.remoteTrigger(context.Background(), remoteTrigger)
		return DirectWebSocketResponse{Type: webSocketActionRemoteTrig, RemoteTrigger: &response}
	default:
		return DirectWebSocketResponse{Type: "error", Error: "unknown websocket action"}
	}
}

func (h *DirectHandler) health() DirectHealthResponse {
	return DirectHealthResponse{
		OK:        true,
		SessionID: h.sessionID,
		Commands:  len(h.manifest.Commands),
	}
}

func (h *DirectHandler) webSocketHello() DirectWebSocketHello {
	actions := []string{
		webSocketActionHello,
		webSocketActionHealth,
		webSocketActionManifest,
		webSocketActionResolve,
		webSocketActionExecute,
	}
	if h.remoteTrigger != nil {
		actions = append(actions, webSocketActionRemoteTrig)
	}
	return DirectWebSocketHello{
		OK:                  true,
		ProtocolVersion:     webSocketProtocolVersion,
		SessionID:           h.sessionID,
		Commands:            len(h.manifest.Commands),
		Capabilities:        append([]Capability(nil), h.manifest.Capabilities...),
		Actions:             actions,
		ManifestGeneratedAt: h.manifest.GeneratedAt,
	}
}

func webSocketAccept(key string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(key) + webSocketMagicGUID))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func headerContainsToken(header string, token string) bool {
	token = strings.ToLower(strings.TrimSpace(token))
	for _, part := range strings.Split(header, ",") {
		if strings.ToLower(strings.TrimSpace(part)) == token {
			return true
		}
	}
	return false
}

func readWebSocketFrame(reader *bufio.Reader) (byte, []byte, error) {
	if reader == nil {
		return 0, nil, os.ErrInvalid
	}
	first, err := reader.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	second, err := reader.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	if first&0x80 == 0 {
		return 0, nil, errors.New("fragmented websocket frames are not supported")
	}
	opcode := first & 0x0F
	masked := second&0x80 != 0
	length := uint64(second & 0x7F)
	switch length {
	case 126:
		var buf [2]byte
		if _, err := io.ReadFull(reader, buf[:]); err != nil {
			return 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(buf[:]))
	case 127:
		var buf [8]byte
		if _, err := io.ReadFull(reader, buf[:]); err != nil {
			return 0, nil, err
		}
		length = binary.BigEndian.Uint64(buf[:])
	}
	if length > webSocketFrameLimit {
		return 0, nil, errors.New("websocket frame exceeds limit")
	}
	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(reader, mask[:]); err != nil {
			return 0, nil, err
		}
	} else {
		return 0, nil, errors.New("client websocket frames must be masked")
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return 0, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return opcode, payload, nil
}

func writeWebSocketJSON(w io.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return writeWebSocketFrame(w, 0x1, data)
}

func writeWebSocketFrame(w io.Writer, opcode byte, payload []byte) error {
	if w == nil {
		return os.ErrInvalid
	}
	var header []byte
	length := len(payload)
	switch {
	case length < 126:
		header = []byte{0x80 | opcode, byte(length)}
	case length <= 0xFFFF:
		header = []byte{0x80 | opcode, 126, 0, 0}
		binary.BigEndian.PutUint16(header[2:], uint16(length))
	default:
		header = []byte{0x80 | opcode, 127, 0, 0, 0, 0, 0, 0, 0, 0}
		binary.BigEndian.PutUint64(header[2:], uint64(length))
	}
	if _, err := w.Write(header); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}
