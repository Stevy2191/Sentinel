// Command agent collects host and container metrics and reports them to a
// Sentinel server.
//
// Configuration comes from the environment so the same binary works under
// systemd, under Docker and by hand, with no config file to keep in step with
// the unit file.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// version is stamped at build time with -ldflags "-X main.version=...".
var version = "dev"

// heartbeatEvery is how often the agent reports in independently of metrics.
//
// Metrics submissions also count as a heartbeat server-side, so this only
// matters when collection is failing — which is exactly when the server should
// still hear that the host is up and the agent is running.
const heartbeatEvery = 5 * time.Minute

// maxQueued bounds the offline backlog. An agent that cannot reach the server
// for a long time must not grow until it is the reason the host runs out of
// memory; the oldest samples are dropped first, since recent history is what
// anyone looks at after an outage.
const maxQueued = 500

type config struct {
	ServerURL     string
	AgentID       string
	ServerToken   string
	ServerName    string
	OSType        string
	CheckInterval time.Duration
	RetryAttempts int
	DiskPath      string
	DockerSocket  string
}

func loadConfig() (config, error) {
	cfg := config{
		// SENTINEL_URL is the documented name. POCKETBASE_URL is accepted
		// because the install commands people copy from other tools use it.
		ServerURL:     firstNonEmpty(os.Getenv("SENTINEL_URL"), os.Getenv("POCKETBASE_URL")),
		AgentID:       strings.TrimSpace(os.Getenv("AGENT_ID")),
		ServerToken:   strings.TrimSpace(os.Getenv("SERVER_TOKEN")),
		ServerName:    strings.TrimSpace(os.Getenv("SERVER_NAME")),
		OSType:        strings.TrimSpace(os.Getenv("OS_TYPE")),
		DiskPath:      envOr("DISK_PATH", "/"),
		DockerSocket:  envOr("DOCKER_SOCKET", "/var/run/docker.sock"),
		RetryAttempts: 3,
		CheckInterval: 60 * time.Second,
	}
	cfg.ServerURL = strings.TrimRight(strings.TrimSpace(cfg.ServerURL), "/")

	var missing []string
	if cfg.ServerURL == "" {
		missing = append(missing, "SENTINEL_URL")
	}
	if cfg.AgentID == "" {
		missing = append(missing, "AGENT_ID")
	}
	if cfg.ServerToken == "" {
		missing = append(missing, "SERVER_TOKEN")
	}
	if len(missing) > 0 {
		return cfg, fmt.Errorf("missing required configuration: %s", strings.Join(missing, ", "))
	}

	if raw := strings.TrimSpace(os.Getenv("CHECK_INTERVAL")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 3600 {
			return cfg, fmt.Errorf("CHECK_INTERVAL must be a whole number of seconds between 1 and 3600, got %q", raw)
		}
		cfg.CheckInterval = time.Duration(n) * time.Second
	}
	if raw := strings.TrimSpace(os.Getenv("RETRY_ATTEMPTS")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 10 {
			return cfg, fmt.Errorf("RETRY_ATTEMPTS must be between 1 and 10, got %q", raw)
		}
		cfg.RetryAttempts = n
	}
	return cfg, nil
}

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)
	log.SetPrefix("[sentinel-agent] ")

	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}

	log.Printf("version %s starting", version)
	log.Printf("server %s, agent %s, interval %s", cfg.ServerURL, cfg.AgentID, cfg.CheckInterval)

	collector := NewCollector(cfg.DiskPath)
	docker := NewDockerCollector(cfg.DockerSocket)
	if docker.Available() {
		log.Printf("docker socket found at %s; container metrics enabled", cfg.DockerSocket)
	} else {
		log.Printf("no docker socket at %s; reporting host metrics only", cfg.DockerSocket)
	}

	client := &apiClient{
		baseURL: cfg.ServerURL,
		token:   cfg.ServerToken,
		agentID: cfg.AgentID,
		retries: cfg.RetryAttempts,
		http:    &http.Client{Timeout: 30 * time.Second},
	}

	// Signals are handled so a stop is a clean exit rather than a kill: an
	// in-flight submission finishes and the queue is not lost mid-write.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	run(ctx, cfg, collector, docker, client)
	log.Println("stopped")
}

func run(ctx context.Context, cfg config, collector *Collector, docker *DockerCollector, client *apiClient) {
	// The first CPU reading only establishes a baseline, so take it now and
	// let the first reported cycle carry a real utilisation figure.
	collector.Collect()

	sysInfo := collectSystemInfo(docker.Available())
	log.Printf("host: %s, %s, %s, %d core(s), %d MB",
		sysInfo.Hostname, sysInfo.OSVersion, sysInfo.Architecture, sysInfo.CPUCores, sysInfo.MemoryTotalMB)

	if err := client.heartbeat(ctx, sysInfo); err != nil {
		// Not fatal. A server that is down at the moment the agent starts is
		// exactly the situation the retry and queue logic exists for.
		log.Printf("initial heartbeat failed (will keep trying): %v", err)
	} else {
		log.Println("registered with server")
	}

	metricsTicker := time.NewTicker(cfg.CheckInterval)
	defer metricsTicker.Stop()
	heartbeatTicker := time.NewTicker(heartbeatEvery)
	defer heartbeatTicker.Stop()

	var queue []Metrics
	for {
		select {
		case <-ctx.Done():
			return

		case <-metricsTicker.C:
			m := collector.Collect()
			if docker.Available() {
				dctx, cancel := context.WithTimeout(ctx, 20*time.Second)
				containers, err := docker.Collect(dctx)
				cancel()
				if err != nil {
					log.Printf("container metrics unavailable this cycle: %v", err)
				} else {
					m.Containers = containers
				}
			}

			queue = append(queue, m)
			if len(queue) > maxQueued {
				dropped := len(queue) - maxQueued
				queue = queue[dropped:]
				log.Printf("backlog full; dropped %d oldest sample(s)", dropped)
			}
			queue = flush(ctx, client, queue)

		case <-heartbeatTicker.C:
			// Re-read rather than reusing what was collected at startup, so a
			// kernel upgrade or added memory shows up without a restart.
			if err := client.heartbeat(ctx, collectSystemInfo(docker.Available())); err != nil {
				log.Printf("heartbeat failed: %v", err)
			}
		}
	}
}

// flush sends queued samples oldest first, stopping at the first failure so
// ordering is preserved and the server is not hammered while it is down.
func flush(ctx context.Context, client *apiClient, queue []Metrics) []Metrics {
	for len(queue) > 0 {
		if err := client.sendMetrics(ctx, queue[0]); err != nil {
			if ctx.Err() != nil {
				return queue
			}
			log.Printf("submission failed, %d sample(s) queued: %v", len(queue), err)
			return queue
		}
		queue = queue[1:]
	}
	return queue
}

// apiClient talks to the Sentinel API.
type apiClient struct {
	baseURL string
	token   string
	agentID string
	retries int
	http    *http.Client
}

func (c *apiClient) heartbeat(ctx context.Context, info SystemInfo) error {
	type heartbeatBody struct {
		SystemInfo
		AgentID      string `json:"agent_id"`
		AgentVersion string `json:"agent_version"`
	}
	return c.post(ctx, "/api/v1/agents/heartbeat", heartbeatBody{
		SystemInfo:   info,
		AgentID:      c.agentID,
		AgentVersion: version,
	})
}

func (c *apiClient) sendMetrics(ctx context.Context, m Metrics) error {
	return c.post(ctx, "/api/v1/agents/"+c.agentID+"/metrics", m)
}

// post sends a request, retrying transient failures with a growing delay.
//
// A rejected credential or a malformed request is not retried: repeating it
// cannot change the answer, and hammering an endpoint with a bad token is how
// an agent gets itself blocked.
func (c *apiClient) post(ctx context.Context, path string, body interface{}) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encoding request: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= c.retries; attempt++ {
		if attempt > 1 {
			delay := time.Duration(attempt-1) * 2 * time.Second
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("building request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("User-Agent", "sentinel-agent/"+version)

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		// Drained before closing so the connection can be reused rather than
		// torn down after every request.
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()

		switch {
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			return nil
		case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
			return fmt.Errorf("rejected by server (%d): check AGENT_ID and SERVER_TOKEN: %s",
				resp.StatusCode, strings.TrimSpace(string(snippet)))
		case resp.StatusCode >= 400 && resp.StatusCode < 500:
			return fmt.Errorf("server rejected the request (%d): %s",
				resp.StatusCode, strings.TrimSpace(string(snippet)))
		default:
			lastErr = fmt.Errorf("server error %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
		}
	}
	if lastErr == nil {
		lastErr = errors.New("request failed")
	}
	return lastErr
}
