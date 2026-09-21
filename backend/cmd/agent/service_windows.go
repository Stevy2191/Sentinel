package main

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/svc"
)

// windowsServiceName must match the name the PowerShell installer registers
// the service under (New-Service -Name SentinelAgent).
const windowsServiceName = "SentinelAgent"

// windowsLogPath is where output goes when running as a service. A Windows
// service has no systemd-style stdout redirection to a file, so the
// installer's post-install check (tailing the log for "registered with
// server") needs the agent to write here itself.
const windowsLogPath = `C:\ProgramData\SentinelAgent\agent.log`

// runAsServiceIfApplicable registers with the Service Control Manager when
// launched as a Windows service, and blocks until the SCM stops it. It
// returns false immediately when run interactively (a console, or the
// Manual install tab's foreground test), so main falls through to the normal
// foreground run() — the same binary works both ways, as it already does on
// Linux under systemd versus a manual invocation.
func runAsServiceIfApplicable(cfg config, collector *Collector, docker *DockerCollector, client *apiClient) bool {
	isService, err := svc.IsWindowsService()
	if err != nil || !isService {
		return false
	}

	if f, err := openServiceLog(); err == nil {
		log.SetOutput(f)
	}

	if err := svc.Run(windowsServiceName, &sentinelService{
		cfg: cfg, collector: collector, docker: docker, client: client,
	}); err != nil {
		log.Printf("service stopped with error: %v", err)
	}
	return true
}

func openServiceLog() (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(windowsLogPath), 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(windowsLogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
}

// sentinelService adapts the existing run() loop to the SCM's Handler
// interface: run() already exits cleanly on context cancellation for
// SIGINT/SIGTERM, so Execute only needs to translate SCM control requests
// into that same cancellation.
type sentinelService struct {
	cfg       config
	collector *Collector
	docker    *DockerCollector
	client    *apiClient
}

func (s *sentinelService) Execute(args []string, r <-chan svc.ChangeRequest, statusChan chan<- svc.Status) (svcSpecificEC bool, exitCode uint32) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		run(ctx, s.cfg, s.collector, s.docker, s.client)
		close(done)
	}()

	statusChan <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

	for {
		select {
		case <-done:
			statusChan <- svc.Status{State: svc.Stopped}
			return false, 0

		case req := <-r:
			switch req.Cmd {
			case svc.Interrogate:
				statusChan <- req.CurrentStatus
			case svc.Stop, svc.Shutdown:
				statusChan <- svc.Status{State: svc.StopPending}
				cancel()
				<-done
				statusChan <- svc.Status{State: svc.Stopped}
				return false, 0
			}
		}
	}
}
