# Windows server agent support

## Problem

The "Operating System" dropdown on Add Server Agent has always offered
"Windows," and the backend's `os_type` check constraint has always accepted
it, but nothing downstream ever implemented it:

- The install instructions shown after creating an agent never branched on
  `os_type` — every OS produced identical bash/systemd/Docker commands.
- The Dockerfile only cross-builds the agent for `GOOS=linux` (amd64/arm64).
- The binary download route is hardcoded to `/agent/download/linux/:arch`.
- The only install scripts are bash (systemd and Docker installers).
- The agent's metrics collector (`collect_linux.go`) is the only
  implementation, and Go's filename-based build constraint excludes it from
  a Windows build entirely — `GOOS=windows go build ./cmd/agent` fails to
  compile today, not merely "unbuilt."

Selecting "Windows" produced instructions that looked identical to Linux
because none of this ever existed.

## Scope

- Host metrics only (CPU, memory, disk, uptime, network). No container
  metrics and no Docker-based install path for Windows — Windows containers
  are a niche, enterprise-only setup unlikely to be in use here.
- Windows Server 2016+ and Windows 10/11, amd64 only (no arm64).
- Persistence via a native Windows Service (`golang.org/x/sys/windows/svc`),
  matching how systemd runs the Linux agent — not a third-party wrapper
  (NSSM) or a Scheduled Task.
- No live Windows machine available during implementation. Verified by
  `GOOS=windows GOARCH=amd64 go build ./cmd/agent` compiling cleanly and by
  careful review of the PowerShell installer; a real install is the
  operator's first live test.

## Design

### Agent source layout

`Metrics`, `Container`, and the `envOr` helper currently live inside
`collect_linux.go`. Because that filename is a Go build constraint
(compiles only under `GOOS=linux`), `main.go` — which must compile on every
target — cannot depend on types defined there once a second OS exists. These
move, unchanged, into a new `metrics.go` (no OS suffix). `collect_linux.go`
keeps its function bodies otherwise untouched.

### `collect_windows.go` (new)

Implements the same function surface as `collect_linux.go`, using
`golang.org/x/sys/windows` (already an indirect dependency; no new heavy
library, consistent with this codebase's preference for direct OS reads
over SDKs — see the comment in `docker.go` about avoiding the Docker SDK):

- CPU: `GetSystemTimes`, same idle/total delta math as the Linux jiffies
  approach.
- Memory: `GlobalMemoryStatusEx`.
- Disk: `GetDiskFreeSpaceEx`, default path `C:\` instead of `/`.
- Uptime: `GetTickCount64`.
- Network: `GetIfTable2`, summed across adapters, excluding loopback and
  virtual adapters (Hyper-V, WSL, VPN, TAP) by interface type/description —
  a heuristic, same spirit as Linux's prefix-based virtual-interface filter.
- Hostname: `os.Hostname()`. OS version, build number, and CPU model: read
  from the registry (`golang.org/x/sys/windows/registry`).
- Load average: omitted (left `nil`). Windows has no equivalent concept;
  the existing code already leaves unavailable metrics unset rather than
  fabricating them.

No changes to `docker.go`: its socket probe (`/var/run/docker.sock`) simply
finds nothing on Windows, so `docker.Available()` is already `false` there.

### `service_windows.go` / `service_other.go` (new)

`golang.org/x/sys/windows/svc` integration so the binary can register with
the Service Control Manager. `service_other.go` (`//go:build !windows`) is a
no-op stub, so Linux's systemd-managed foreground process is unchanged.
`main.go` gains one call to this before its existing `run()` call; when it
reports "handled," `main` returns without also calling `run()` directly.

When running as a service, output is redirected to
`C:\ProgramData\SentinelAgent\agent.log`, since Windows services have no
systemd-style stdout redirection. This is what lets the installer verify
success by tailing the log, matching the Linux script's behavior.

### Backend

- `Dockerfile`: one more cross-build line,
  `GOOS=windows GOARCH=amd64 ... -o sentinel-agent-windows-amd64.exe`.
- `agent_install_handler.go`: new route `/agent/download/windows/:arch`
  (amd64 only) beside the existing Linux one, which is unchanged.
- `agent_install_scripts.go`: new `server-agent.ps1` template, mirroring
  the bash installer step-for-step — reachability check, download, install
  to `C:\Program Files\SentinelAgent`, register the service, write
  configuration via the service's registry `Environment` value (the
  officially-supported mechanism for per-service env vars — no agent code
  changes needed for this), start it, then tail the log to confirm
  "registered with server" before declaring success. Written against
  PowerShell 5.1 syntax (what Server 2016+ and Win10/11 ship by default).

### Frontend

`InstallStep` in `AddServerAgentModal.tsx` branches on `agent.os_type`. For
Windows: tabs are `One-Click Install` (PowerShell/service), `Manual`, and
`Uninstall` — the two Docker tabs are hidden. Commands render PowerShell
instead of bash, with matching prerequisite text (elevated PowerShell
instead of `sudo` + systemd).

## Testing

- `GOOS=windows GOARCH=amd64 go build ./cmd/agent` inside a `golang:1.26-alpine`
  container (no local Go toolchain on this host, but Docker is available).
- Existing backend test suite (`go test ./...`) must still pass after the
  `metrics.go` extraction.
- No `pwsh` available in this environment, so `server-agent.ps1` is verified
  by careful manual review against PowerShell 5.1 syntax, not executed. The
  first real install on a Windows machine is the operator's responsibility.
