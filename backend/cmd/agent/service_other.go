//go:build !windows

package main

// runAsServiceIfApplicable is a Windows-only concept: on every other
// platform the process model (systemd, Docker, or a bare foreground run) is
// already handled by run() and the caller's own supervisor, so this is
// always a no-op and main() always falls through to run() directly.
func runAsServiceIfApplicable(cfg config, collector *Collector, docker *DockerCollector, client *apiClient) bool {
	return false
}
