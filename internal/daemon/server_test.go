package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestServerHealthAndStatus(t *testing.T) {
	state := BuildState("sess_daemon_server", "/work", RuntimeRunning, 1234, "http://127.0.0.1:1", time.Now().UTC(), nil)
	server, err := StartServer(ServerOptions{
		StateFunc: func() State { return state },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := server.Close(ctx); err != nil {
			t.Fatalf("close daemon server: %v", err)
		}
	})
	healthResp, err := http.Get(server.Endpoint() + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer healthResp.Body.Close()
	if healthResp.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d", healthResp.StatusCode)
	}
	var health HealthResponse
	if err := json.NewDecoder(healthResp.Body).Decode(&health); err != nil {
		t.Fatal(err)
	}
	if !health.OK || health.SessionID != "sess_daemon_server" || health.RuntimeState != RuntimeRunning || health.PID != 1234 {
		t.Fatalf("health = %#v", health)
	}

	statusResp, err := http.Get(server.Endpoint() + "/status")
	if err != nil {
		t.Fatal(err)
	}
	defer statusResp.Body.Close()
	var status State
	if err := json.NewDecoder(statusResp.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if status.SessionID != "sess_daemon_server" || status.Endpoint != "http://127.0.0.1:1" {
		t.Fatalf("status = %#v", status)
	}
}

func TestStartServerRejectsNonLoopbackAddress(t *testing.T) {
	_, err := StartServer(ServerOptions{Addr: "0.0.0.0:0"})
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("err = %v", err)
	}
}

func TestServerTick(t *testing.T) {
	var called bool
	server, err := StartServer(ServerOptions{
		TickFunc: func(context.Context) TickResponse {
			called = true
			return TickResponse{OK: true, CheckedAt: "2026-06-17T10:00:00Z", TriggeredCount: 2}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := server.Close(ctx); err != nil {
			t.Fatalf("close daemon server: %v", err)
		}
	})
	resp, err := http.Post(server.Endpoint()+"/tick", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("tick status = %d", resp.StatusCode)
	}
	var tick TickResponse
	if err := json.NewDecoder(resp.Body).Decode(&tick); err != nil {
		t.Fatal(err)
	}
	if !called || !tick.OK || tick.TriggeredCount != 2 {
		t.Fatalf("called=%v tick=%#v", called, tick)
	}
}

func TestServerTickRequiresCallback(t *testing.T) {
	server, err := StartServer(ServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := server.Close(ctx); err != nil {
			t.Fatalf("close daemon server: %v", err)
		}
	})
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/tick", nil)
	server.server.Handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("tick status = %d body=%s", recorder.Code, recorder.Body.String())
	}
}
