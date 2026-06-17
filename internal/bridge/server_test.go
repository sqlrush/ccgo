package bridge

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestStartDirectServerListensOnLoopbackAndServesHealth(t *testing.T) {
	server, err := StartDirectServer(DirectServerOptions{
		Handler: NewDirectHandler(DirectOptions{
			SessionID: "sess_bridge",
			Manifest:  testDirectManifest(t),
			Registry:  testDirectRegistry(),
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := server.Close(ctx); err != nil {
			t.Fatalf("close direct server: %v", err)
		}
	})
	if server.URL() == "" || server.Addr() == nil {
		t.Fatalf("server url=%q addr=%v", server.URL(), server.Addr())
	}
	resp, err := http.Get(server.URL() + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var health DirectHealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatal(err)
	}
	if !health.OK || health.SessionID != "sess_bridge" {
		t.Fatalf("health = %#v", health)
	}
}

func TestStartDirectServerRejectsNonLoopbackAddr(t *testing.T) {
	_, err := StartDirectServer(DirectServerOptions{
		Addr:    "0.0.0.0:0",
		Handler: NewDirectHandler(DirectOptions{Manifest: testDirectManifest(t), Registry: testDirectRegistry()}),
	})
	if err == nil {
		t.Fatal("StartDirectServer accepted non-loopback address")
	}
}

func TestDirectServerTokenGuard(t *testing.T) {
	server, err := StartDirectServer(DirectServerOptions{
		Token: "secret",
		Handler: NewDirectHandler(DirectOptions{
			SessionID: "sess_bridge",
			Manifest:  testDirectManifest(t),
			Registry:  testDirectRegistry(),
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := server.Close(ctx); err != nil {
			t.Fatalf("close direct server: %v", err)
		}
	})
	resp, err := http.Get(server.URL() + "/health")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", resp.StatusCode)
	}

	req, err := http.NewRequest(http.MethodGet, server.URL()+"/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authorized status = %d", resp.StatusCode)
	}
}

func TestDirectTokenGuardAcceptsBridgeTokenHeader(t *testing.T) {
	server, err := StartDirectServer(DirectServerOptions{
		Token: "secret",
		Handler: NewDirectHandler(DirectOptions{
			SessionID: "sess_bridge",
			Manifest:  testDirectManifest(t),
			Registry:  testDirectRegistry(),
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := server.Close(ctx); err != nil {
			t.Fatalf("close direct server: %v", err)
		}
	})
	req, err := http.NewRequest(http.MethodGet, server.URL()+"/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Bridge-Token", "secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestValidateLoopbackListenAddr(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:0", "localhost:0", "[::1]:0"} {
		if err := validateLoopbackListenAddr(addr); err != nil {
			t.Fatalf("validateLoopbackListenAddr(%q): %v", addr, err)
		}
	}
	for _, addr := range []string{":0", "0.0.0.0:0", "example.com:80", "not-host-port"} {
		if err := validateLoopbackListenAddr(addr); err == nil {
			t.Fatalf("validateLoopbackListenAddr(%q) accepted", addr)
		}
	}
}
