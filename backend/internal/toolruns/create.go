package toolruns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/nettools"
)

// Create validates the request, applies the guardrails in order (parameters,
// vantage, target resolution, allowlist, per-target rate, active-run caps),
// inserts the run and starts it on the server or queues it for the agent.
// User-facing failures are *Refusal.
func (s *Service) Create(ctx context.Context, who Requester, req CreateRequest) (*models.ToolRun, error) {
	spec, err := nettools.Normalize(nettools.Spec{Tool: req.Tool, Target: req.Target, Params: req.Params})
	if err != nil {
		return nil, refuse(http.StatusUnprocessableEntity, CodeInvalidParams, "%s", err.Error())
	}
	settings := LoadSettings(ctx, s.settings)
	now := s.now()

	// The vantage.
	var agent *models.Agent
	vantageName := sentinelVantageName
	switch req.Vantage.Kind {
	case models.VantageSentinel:
		if !settings.ServerEnabled {
			return nil, refuse(http.StatusConflict, CodeVantageNotReady, "%s", reasonText(ReasonServerDisabled, ""))
		}
	case models.VantageAgent:
		var a models.Agent
		err := s.db.WithContext(ctx).First(&a, "agent_id = ?", req.Vantage.AgentID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, refuse(http.StatusNotFound, CodeNotFound, "no such agent")
		}
		if err != nil {
			return nil, fmt.Errorf("loading agent %s: %w", req.Vantage.AgentID, err)
		}
		if reason := s.agentReason(&a, now); reason != "" {
			return nil, refuse(http.StatusConflict, CodeVantageNotReady, "%s", reasonText(reason, a.Name))
		}
		agent, vantageName = &a, a.Name
	default:
		return nil, refuse(http.StatusUnprocessableEntity, CodeInvalidParams, "vantage: kind must be sentinel or agent")
	}

	// The address the tool will contact, checked against the allowlist. A
	// DNS lookup through the vantage's own resolver contacts no target.
	host := spec.Target
	if spec.Tool == nettools.ToolDNS {
		host = dnsServerHost(spec.Params.Server)
	}
	var targetIP *string
	if host != "" {
		ip, refusal := s.resolveTarget(ctx, host)
		if refusal != nil {
			return nil, refusal
		}
		allow, _ := nettools.ParseAllowlist(settings.Allowlist)
		if allow.Empty() || !allow.Allows(host, ip) {
			r := refuse(http.StatusUnprocessableEntity, CodeTargetNotAllowed,
				"%s is not on the network tools allowlist", describeTarget(host, ip))
			s.audit.Record(ctx, who.actor(), models.ActionToolRunRefused, models.ResourceToolRun, nil,
				models.AuditChanges{Summary: map[string]any{
					"tool": string(spec.Tool), "target": spec.Target, "target_ip": ip.String(),
					"vantage": vantageAudit(req.Vantage.Kind, agent), "code": r.Code, "message": r.Message,
				}})
			return nil, r
		}
		addr := ip.String()
		targetIP = &addr
		spec.TargetIP = addr
		if !s.targets.allow(addr, now) {
			return nil, refuse(http.StatusTooManyRequests, CodeLimit, "too many runs against %s; wait a minute", addr)
		}
	}

	params, err := json.Marshal(spec.Params)
	if err != nil {
		return nil, fmt.Errorf("encoding parameters: %w", err)
	}
	userID := who.UserID
	run := &models.ToolRun{
		ID:          uuid.New(),
		Tool:        string(spec.Tool),
		UserID:      &userID,
		Username:    who.Username,
		VantageKind: req.Vantage.Kind,
		VantageName: vantageName,
		Target:      spec.Target,
		TargetIP:    targetIP,
		Params:      models.RawJSON(params),
	}
	if agent != nil {
		run.Status = models.ToolRunQueued
		run.AgentUUID, run.AgentRef = &agent.ID, &agent.AgentID
	} else {
		run.Status = models.ToolRunRunning
	}

	var capRefusal *Refusal
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Serialises every create's cap check and insert, so concurrent
		// creates cannot overshoot a cap. Released at commit.
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext('tool_runs_caps'))`).Error; err != nil {
			return fmt.Errorf("taking the tool runs lock: %w", err)
		}
		var agentID *uuid.UUID
		agentName := ""
		if agent != nil {
			agentID, agentName = &agent.ID, agent.Name
		}
		r, err := checkCaps(tx, who.UserID, targetIP, agentID, agentName)
		if err != nil {
			return err
		}
		if r != nil {
			capRefusal = r
			return r
		}
		// The run's clock starts here, after the lookup and the wait for
		// the lock, so neither eats into the tool's time limit (a
		// maximum-length ping has only 5 s to spare).
		start := s.now()
		run.CreatedAt = start
		if agent != nil {
			// Queued until the agent claims it; the claim resets the
			// deadline to claim time + the tool's limit. Until then it is far
			// enough out that the pickup timeout, not the deadline, ends an
			// unclaimed run.
			run.Deadline = start.Add(PickupTimeout + nettools.Deadline(spec.Tool))
		} else {
			run.StartedAt = &start
			run.Deadline = start.Add(nettools.Deadline(spec.Tool))
		}
		return tx.Create(run).Error
	})
	if capRefusal != nil {
		return nil, capRefusal
	}
	if err != nil {
		return nil, fmt.Errorf("creating tool run: %w", err)
	}

	s.audit.Record(ctx, who.actor(), models.ActionToolRunStarted, models.ResourceToolRun, &run.ID,
		models.AuditChanges{Summary: map[string]any{
			"tool": run.Tool, "target": run.Target, "target_ip": targetIP,
			"vantage": vantageAudit(run.VantageKind, agent), "params": json.RawMessage(params),
		}})

	if agent != nil {
		s.wake(agent.ID)
	} else {
		s.launch(run, spec)
	}
	return run, nil
}

// dnsServerHost is the address part of a DNS lookup's server parameter
// ("a.b.c.d" or "a.b.c.d:port"), or "" for the system resolver.
func dnsServerHost(server string) string {
	if server == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(server); err == nil {
		return host
	}
	return server
}

// resolveTarget turns the typed target into the one IPv4 address the run
// will contact: a literal address as is, a name through Resolve (first IPv4
// answer). Resolved once, here: what was checked is what gets probed.
func (s *Service) resolveTarget(ctx context.Context, host string) (net.IP, *Refusal) {
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			return v4, nil
		}
		return nil, refuse(http.StatusUnprocessableEntity, CodeIPv6Unsupported,
			"%s is an IPv6 address; S1 tools are IPv4 only", host)
	}
	rctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()
	ips, err := s.resolve(rctx, host)
	if err != nil || len(ips) == 0 {
		return nil, refuse(http.StatusUnprocessableEntity, CodeResolveFailed,
			"%s doesn't resolve from Sentinel — use its IP address", host)
	}
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			return v4, nil
		}
	}
	return nil, refuse(http.StatusUnprocessableEntity, CodeIPv6Unsupported,
		"%s only has IPv6 addresses; S1 tools are IPv4 only", host)
}

// describeTarget is "name (address)", or just the address when that is
// what was typed.
func describeTarget(host string, ip net.IP) string {
	if host == ip.String() {
		return host
	}
	return host + " (" + ip.String() + ")"
}

// vantageAudit describes the vantage for an audit entry.
func vantageAudit(kind string, agent *models.Agent) map[string]any {
	if agent == nil {
		return map[string]any{"kind": kind}
	}
	return map[string]any{"kind": kind, "agent_id": agent.AgentID, "agent_uuid": agent.ID, "name": agent.Name}
}
