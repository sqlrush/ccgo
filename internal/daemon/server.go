package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"

	"ccgo/internal/contracts"
)

type ServerOptions struct {
	Addr      string
	StateFunc func() State
	TickFunc  func(context.Context) TickResponse
}

type Server struct {
	listener net.Listener
	server   *http.Server
}

type HealthResponse struct {
	OK           bool         `json:"ok"`
	SessionID    contracts.ID `json:"session_id,omitempty"`
	RuntimeState string       `json:"runtime_state,omitempty"`
	PID          int          `json:"pid,omitempty"`
}

type TickResponse struct {
	OK             bool           `json:"ok"`
	CheckedAt      string         `json:"checked_at,omitempty"`
	TriggeredCount int            `json:"triggered_count,omitempty"`
	ErrorCount     int            `json:"error_count,omitempty"`
	Structured     map[string]any `json:"structured,omitempty"`
	Error          string         `json:"error,omitempty"`
}

func StartServer(options ServerOptions) (*Server, error) {
	addr := strings.TrimSpace(options.Addr)
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	if err := validateLoopbackAddr(addr); err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	stateFunc := options.StateFunc
	if stateFunc == nil {
		stateFunc = func() State { return State{RuntimeState: RuntimeDisabled} }
	}
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		state := stateFunc()
		writeJSON(w, http.StatusOK, HealthResponse{
			OK:           true,
			SessionID:    state.SessionID,
			RuntimeState: state.RuntimeState,
			PID:          state.PID,
		})
	})
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		writeJSON(w, http.StatusOK, stateFunc())
	})
	mux.HandleFunc("/tick", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		if options.TickFunc == nil {
			writeJSON(w, http.StatusNotImplemented, TickResponse{OK: false, Error: "daemon tick is not configured"})
			return
		}
		response := options.TickFunc(r.Context())
		status := http.StatusOK
		if !response.OK {
			status = http.StatusInternalServerError
		}
		writeJSON(w, status, response)
	})
	server := &http.Server{Handler: mux}
	wrapped := &Server{listener: listener, server: server}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			_ = listener.Close()
		}
	}()
	return wrapped, nil
}

func (s *Server) Endpoint() string {
	if s == nil || s.listener == nil {
		return ""
	}
	return "http://" + s.listener.Addr().String()
}

func (s *Server) Close(ctx context.Context) error {
	if s == nil || s.server == nil {
		return nil
	}
	return s.server.Shutdown(ctx)
}

func validateLoopbackAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid daemon address %q: %w", addr, err)
	}
	if host == "" {
		return fmt.Errorf("daemon address must bind loopback, got %q", addr)
	}
	ip := net.ParseIP(host)
	if ip != nil {
		if ip.IsLoopback() {
			return nil
		}
		return fmt.Errorf("daemon address must bind loopback, got %q", addr)
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	return fmt.Errorf("daemon address must bind loopback, got %q", addr)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
