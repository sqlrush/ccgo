package daemon

import (
	"context"
	"encoding/json"
	"net/http"
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
