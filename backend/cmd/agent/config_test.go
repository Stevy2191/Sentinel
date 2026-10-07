package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/nettools"
)

func TestLoadConfigReadsTheToolSettings(t *testing.T) {
	base := func(t *testing.T) {
		t.Setenv("SENTINEL_URL", "http://10.1.20.10:3001")
		t.Setenv("AGENT_ID", testAgentID)
		t.Setenv("SERVER_TOKEN", testToken)
		t.Setenv("CHECK_INTERVAL", "")
		t.Setenv("RETRY_ATTEMPTS", "")
		t.Setenv("ENABLE_TOOLS", "")
		t.Setenv("TOOLS_ALLOWED_TARGETS", "")
	}
	for raw, want := range map[string]bool{
		"": false, "true": true, "TRUE": true, "1": true, "yes": true, "Yes": true,
		"false": false, "0": false, "no": false, "on": false,
	} {
		t.Run("ENABLE_TOOLS="+raw, func(t *testing.T) {
			base(t)
			t.Setenv("ENABLE_TOOLS", raw)
			cfg, err := loadConfig()
			if err != nil || cfg.ToolsEnabled != want {
				t.Errorf("ToolsEnabled = %v, %v; want %v", cfg.ToolsEnabled, err, want)
			}
		})
	}
	t.Run("allowed targets", func(t *testing.T) {
		base(t)
		t.Setenv("TOOLS_ALLOWED_TARGETS", "10.0.0.0/8,192.168.1.10")
		cfg, err := loadConfig()
		if err != nil || len(cfg.ToolsAllowed) != 2 {
			t.Fatalf("ToolsAllowed = %v, %v; want two networks", cfg.ToolsAllowed, err)
		}
		for ip, want := range map[string]bool{"10.1.2.3": true, "192.168.1.10": true, "192.168.1.11": false} {
			if got := nettools.InNets(net.ParseIP(ip), cfg.ToolsAllowed); got != want {
				t.Errorf("%s allowed = %v, want %v", ip, got, want)
			}
		}
	})
	t.Run("bad allowed targets", func(t *testing.T) {
		base(t)
		t.Setenv("TOOLS_ALLOWED_TARGETS", "10.0.0.0/33")
		if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), "TOOLS_ALLOWED_TARGETS") {
			t.Errorf("err = %v, want one naming TOOLS_ALLOWED_TARGETS", err)
		}
	})
}

// Sentinel tells "too old to run tools" (no tools_local at all) from "tools
// off on this host" (false), so the field is always sent.
func TestHeartbeatReportsToolsLocal(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		f := newFakeSentinel(t)
		c := &apiClient{baseURL: f.srv.URL, token: testToken, agentID: testAgentID, retries: 1,
			http: &http.Client{Timeout: 5 * time.Second}, toolsLocal: enabled}
		if err := c.heartbeat(context.Background(), SystemInfo{Hostname: "files"}); err != nil {
			t.Fatal(err)
		}
		f.mu.Lock()
		hb := f.heartbeats
		f.mu.Unlock()
		if len(hb) != 1 {
			t.Fatalf("%d heartbeats, want 1", len(hb))
		}
		if got, ok := hb[0]["tools_local"]; !ok || got != enabled {
			t.Errorf("tools_local = %v (present %v), want %v", got, ok, enabled)
		}
	}
}

// Without ENABLE_TOOLS the agent never asks for jobs; with it, it does.
func TestRunCollectsJobsOnlyWithEnableTools(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("ENABLE_TOOLS=%v", enabled), func(t *testing.T) {
			f := newFakeSentinel(t)
			cfg := config{ServerURL: f.srv.URL, AgentID: testAgentID, ServerToken: testToken,
				CheckInterval: time.Hour, RetryAttempts: 1, ToolsEnabled: enabled}
			client := &apiClient{baseURL: cfg.ServerURL, token: testToken, agentID: testAgentID, retries: 1,
				http: &http.Client{Timeout: 5 * time.Second}, toolsLocal: enabled}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() {
				run(ctx, cfg, NewCollector("/"), NewDockerCollector(filepath.Join(t.TempDir(), "no-docker.sock")), client)
				close(done)
			}()
			defer func() {
				cancel()
				<-done
			}()

			waitFor(t, "the first heartbeat", func() bool {
				f.mu.Lock()
				defer f.mu.Unlock()
				return len(f.heartbeats) == 1
			})
			if enabled {
				waitFor(t, "a jobs poll", func() bool { return f.pollCount() >= 1 })
				return
			}
			time.Sleep(200 * time.Millisecond)
			if n := f.pollCount(); n != 0 {
				t.Errorf("%d jobs polls without ENABLE_TOOLS, want none", n)
			}
		})
	}
}
