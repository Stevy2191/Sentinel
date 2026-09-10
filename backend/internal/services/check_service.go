// Package services contains Sentinel's business logic, including execution of
// monitor checks and persistence of their results.
package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/netguard"
)

// Check result status values.
const (
	checkSuccess = "success"
	checkFailed  = "failed"
	checkTimeout = "timeout"
)

const (
	// defaultHTTPClientTimeout bounds the shared client used for ad-hoc requests.
	defaultHTTPClientTimeout = 30 * time.Second
	// defaultCheckTimeout is used when a monitor does not specify a timeout.
	defaultCheckTimeout = 10 * time.Second
	// userAgent identifies Sentinel's probes to remote servers.
	userAgent = "Sentinel/1.0"
	// protocolICMP is the IANA protocol number for ICMPv4, used when parsing replies.
	protocolICMP = 1
)

// CheckService executes monitor checks across all supported protocols and
// persists their results.
type CheckService struct {
	db     *gorm.DB
	logger *log.Logger
}

// NewCheckService returns a CheckService backed by the given database, with an
// HTTP client using a 30-second default timeout.
func NewCheckService(db *gorm.DB) *CheckService {
	return &CheckService{
		db:     db,
		logger: log.Default(),
	}
}

// ExecuteCheck routes to the protocol-specific check for the monitor's type,
// logs the total execution time, and recovers from any panic by returning it as
// an error rather than crashing the caller.
func (s *CheckService) ExecuteCheck(ctx context.Context, monitor *models.Monitor) (check *models.Check, err error) {
	start := time.Now()
	defer func() {
		if r := recover(); r != nil {
			check = nil
			err = fmt.Errorf("panic during check execution for monitor %s: %v", monitor.ID, r)
		}
		s.logger.Printf("[check] monitor=%s type=%s duration=%s", monitor.ID, monitor.Type, time.Since(start))
	}()

	switch monitor.Type {
	case models.MonitorTypeHTTP, models.MonitorTypeWebhook:
		return s.ExecuteHTTPCheck(ctx, monitor)
	case models.MonitorTypeTCP:
		return s.ExecuteTCPCheck(ctx, monitor)
	case models.MonitorTypePing:
		return s.ExecutePingCheck(ctx, monitor)
	case models.MonitorTypeDNS:
		return s.ExecuteDNSCheck(ctx, monitor)
	default:
		return nil, fmt.Errorf("unsupported monitor type %q", monitor.Type)
	}
}

// ExecuteHTTPCheck performs an HTTP(S) request against the monitor's URL,
// retrying up to monitor.Retries times with exponential backoff. A completed
// probe (including a non-2xx or timeout outcome) is returned as a Check with a
// nil error; a non-nil error indicates the check could not be attempted.
func (s *CheckService) ExecuteHTTPCheck(ctx context.Context, monitor *models.Monitor) (*models.Check, error) {
	timeout := s.checkTimeout(monitor)
	// Certificate verification is per-monitor: on unless someone deliberately
	// turned it off for this target.
	client := netguard.NewGuardedHTTPClientTLS(timeout, monitor.VerifyTLS())

	method := strings.ToUpper(strings.TrimSpace(monitor.Method))
	if method == "" {
		method = http.MethodGet
	}

	attempts := monitor.Retries + 1
	var lastCheck *models.Check

	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			s.logger.Printf("[http] monitor=%s retry %d/%d after %s", monitor.ID, attempt, monitor.Retries, backoff)
			select {
			case <-ctx.Done():
				return s.timeoutCheck(monitor, 0, ctx.Err()), nil
			case <-time.After(backoff):
			}
		}

		check, retriable := s.httpAttempt(ctx, monitor, client, method, attempt)
		if !retriable {
			return check, nil
		}
		lastCheck = check
	}

	return lastCheck, nil
}

// httpAttempt performs a single HTTP request. It returns the resulting Check and
// whether the outcome is worth retrying (true for failures/timeouts, false for
// success).
func (s *CheckService) httpAttempt(ctx context.Context, monitor *models.Monitor, client *http.Client, method string, attempt int) (*models.Check, bool) {
	reqCtx, cancel := context.WithTimeout(ctx, s.checkTimeout(monitor))
	defer cancel()

	var body io.Reader
	if monitor.Body != "" {
		body = strings.NewReader(monitor.Body)
	}

	req, err := http.NewRequestWithContext(reqCtx, method, monitor.URL, body)
	if err != nil {
		s.logger.Printf("[http] monitor=%s attempt=%d build-request-error=%v", monitor.ID, attempt, err)
		return s.failedCheck(monitor, 0, 0, fmt.Sprintf("building request: %v", err)), true
	}

	for key, value := range monitor.Headers {
		req.Header.Set(key, value)
	}
	req.Header.Set("User-Agent", userAgent)

	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		if isTimeout(err) {
			s.logger.Printf("[http] monitor=%s attempt=%d timeout after %s", monitor.ID, attempt, elapsed)
			return s.timeoutCheck(monitor, int(elapsed.Milliseconds()), err), true
		}
		s.logger.Printf("[http] monitor=%s attempt=%d connection-error=%v", monitor.ID, attempt, err)
		return s.failedCheck(monitor, elapsed, 0, err.Error()), true
	}
	defer resp.Body.Close()

	bodyLen, _ := io.Copy(io.Discard, resp.Body)
	ms := int(elapsed.Milliseconds())
	s.logger.Printf("[http] monitor=%s attempt=%d status=%d time=%dms body_len=%d", monitor.ID, attempt, resp.StatusCode, ms, bodyLen)

	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
		check := s.successCheck(monitor, elapsed, resp.StatusCode)
		return check, false
	}

	check := s.failedCheck(monitor, elapsed, resp.StatusCode, fmt.Sprintf("unexpected status code %d", resp.StatusCode))
	return check, true
}

// ExecuteTCPCheck opens a TCP connection to the monitor's host:port and reports
// whether it succeeds within the timeout.
// retryProbe runs a probe up to the monitor's configured attempts, returning
// the first success or the last failure.
//
// Shared by the TCP and DNS checks. Both previously ran once whatever the
// monitor's retry setting said, so a single dropped SYN or a single slow
// resolver answer opened an incident and alerted, exactly as a single lost
// ICMP packet did.
func (s *CheckService) retryProbe(
	ctx context.Context,
	monitor *models.Monitor,
	kind string,
	probe func(ctx context.Context, budget time.Duration) *models.Check,
) *models.Check {
	attempts := probeAttempts(monitor)
	budget := probeBudget(s.checkTimeout(monitor), attempts)

	var last *models.Check
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return s.timeoutCheck(monitor, 0, ctx.Err())
			case <-time.After(retryBackoff(attempt)):
			}
		}

		check := probe(ctx, budget)
		if check.Status == checkSuccess {
			if attempt > 0 {
				s.logger.Printf("[%s] monitor=%s succeeded on attempt %d of %d",
					kind, monitor.ID, attempt+1, attempts)
			}
			return check
		}
		last = check
	}

	if last != nil && attempts > 1 {
		last.ErrorMessage = fmt.Sprintf("failed %d attempts: %s", attempts, last.ErrorMessage)
	}
	return last
}

func (s *CheckService) ExecuteTCPCheck(ctx context.Context, monitor *models.Monitor) (*models.Check, error) {
	address, err := hostPort(monitor.URL, "")
	if err != nil {
		return s.failedCheck(monitor, 0, 0, err.Error()), nil
	}

	return s.retryProbe(ctx, monitor, "tcp", func(ctx context.Context, budget time.Duration) *models.Check {
		dialCtx, cancel := context.WithTimeout(ctx, budget)
		defer cancel()

		dialer := netguard.SafeDialer(budget)
		start := time.Now()
		conn, err := dialer.DialContext(dialCtx, "tcp", address)
		elapsed := time.Since(start)
		if err != nil {
			if isTimeout(err) {
				s.logger.Printf("[tcp] monitor=%s addr=%s timeout after %s", monitor.ID, address, elapsed)
				return s.timeoutCheck(monitor, int(elapsed.Milliseconds()), err)
			}
			s.logger.Printf("[tcp] monitor=%s addr=%s error=%v", monitor.ID, address, err)
			return s.failedCheck(monitor, elapsed, 0, err.Error())
		}
		_ = conn.Close()

		s.logger.Printf("[tcp] monitor=%s addr=%s connected in %dms", monitor.ID, address, elapsed.Milliseconds())
		return s.successCheck(monitor, elapsed, 0)
	}), nil
}

// ExecutePingCheck resolves the monitor's host and sends an ICMP echo request.
// If a raw/unprivileged ICMP socket cannot be opened, it falls back to a TCP
// connect on port 80.
func (s *CheckService) ExecutePingCheck(ctx context.Context, monitor *models.Monitor) (*models.Check, error) {
	timeout := s.checkTimeout(monitor)
	host := hostname(monitor.URL)

	resolveCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupHost(resolveCtx, host)
	if err != nil {
		if isTimeout(err) {
			return s.timeoutCheck(monitor, 0, err), nil
		}
		s.logger.Printf("[ping] monitor=%s host=%s resolve-error=%v", monitor.ID, host, err)
		return s.failedCheck(monitor, 0, 0, fmt.Sprintf("resolving host: %v", err)), nil
	}
	target := addrs[0]
	if ip := net.ParseIP(target); netguard.IsBlocked(ip) {
		s.logger.Printf("[ping] monitor=%s host=%s target=%s blocked by netguard", monitor.ID, host, target)
		return s.failedCheck(monitor, 0, 0, "target address is not allowed (blocked network range)"), nil
	}

	// One lost packet is not an outage. ICMP is unreliable by design and
	// routers deprioritise it, so a small amount of loss on a long path is
	// normal — a single probe made every such loss look like the host was
	// down. Several probes are sent and any reply counts, which is what
	// `ping -c 3` does and what an operator checking by hand would accept.
	attempts := probeAttempts(monitor)
	budget := probeBudget(timeout, attempts)

	var last *models.Check
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return s.timeoutCheck(monitor, 0, ctx.Err()), nil
			case <-time.After(retryBackoff(attempt)):
			}
		}

		check, err := s.icmpPing(ctx, monitor, target, budget, attempt)
		if err != nil {
			s.logger.Printf("[ping] monitor=%s ICMP unavailable (%v); falling back to TCP:80", monitor.ID, err)
			return s.tcpConnect(ctx, monitor, net.JoinHostPort(host, "80"), timeout)
		}
		if check.Status == checkSuccess {
			if attempt > 0 {
				s.logger.Printf("[ping] monitor=%s host=%s replied on probe %d of %d",
					monitor.ID, target, attempt+1, attempts)
			}
			return check, nil
		}
		last = check
	}

	// Every probe went unanswered. The message says how many, so a reader can
	// tell a single lost packet from a host that is not answering at all.
	if last != nil && attempts > 1 {
		last.ErrorMessage = fmt.Sprintf("no reply to %d probes: %s", attempts, last.ErrorMessage)
	}
	s.logger.Printf("[ping] monitor=%s host=%s no reply to %d probe(s)", monitor.ID, target, attempts)
	return last, nil
}

// icmpPing sends a single ICMP echo request to ipAddr and waits for a reply. A
// setup error (e.g. insufficient privileges) is returned so the caller can fall
// back; a completed probe is returned as a Check with a nil error.
func (s *CheckService) icmpPing(ctx context.Context, monitor *models.Monitor, ipAddr string, timeout time.Duration, seq int) (*models.Check, error) {
	privileged := false
	conn, err := icmp.ListenPacket("udp4", "0.0.0.0")
	if err != nil {
		conn, err = icmp.ListenPacket("ip4:icmp", "0.0.0.0")
		if err != nil {
			return nil, fmt.Errorf("opening ICMP socket: %w", err)
		}
		privileged = true
	}
	defer conn.Close()

	ip := net.ParseIP(ipAddr)
	if ip == nil {
		return nil, fmt.Errorf("invalid IP address %q", ipAddr)
	}

	var dst net.Addr = &net.UDPAddr{IP: ip}
	if privileged {
		dst = &net.IPAddr{IP: ip}
	}

	msg := icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{
			ID: os.Getpid() & 0xffff,
			// Distinct per probe, so a reply to an earlier attempt that
			// arrived late is not mistaken for a reply to this one.
			Seq:  seq & 0xffff,
			Data: []byte("SENTINEL-PING"),
		},
	}
	wb, err := msg.Marshal(nil)
	if err != nil {
		return nil, fmt.Errorf("marshaling ICMP message: %w", err)
	}

	deadline := time.Now().Add(timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("setting ICMP deadline: %w", err)
	}

	start := time.Now()
	if _, err := conn.WriteTo(wb, dst); err != nil {
		return nil, fmt.Errorf("sending ICMP echo: %w", err)
	}

	reply := make([]byte, 1500)
	for {
		n, _, err := conn.ReadFrom(reply)
		if err != nil {
			if isTimeout(err) {
				s.logger.Printf("[ping] monitor=%s host=%s timeout", monitor.ID, ipAddr)
				return s.timeoutCheck(monitor, int(time.Since(start).Milliseconds()), err), nil
			}
			return s.failedCheck(monitor, time.Since(start), 0, err.Error()), nil
		}

		parsed, err := icmp.ParseMessage(protocolICMP, reply[:n])
		if err != nil {
			continue
		}
		if parsed.Type == ipv4.ICMPTypeEchoReply {
			// Match the reply to the probe. A raw ICMP socket receives every
			// echo reply arriving at the host, including replies to other
			// processes and to this monitor's own earlier attempts, so
			// accepting any of them would report a success that never
			// happened. The unprivileged socket has the kernel rewrite the id,
			// so only the sequence number is checked there.
			echo, ok := parsed.Body.(*icmp.Echo)
			if !ok {
				continue
			}
			if echo.Seq != (seq & 0xffff) {
				continue
			}
			if privileged && echo.ID != (os.Getpid()&0xffff) {
				continue
			}

			elapsed := time.Since(start)
			s.logger.Printf("[ping] monitor=%s host=%s rtt=%dms", monitor.ID, ipAddr, elapsed.Milliseconds())
			return s.successCheck(monitor, elapsed, 0), nil
		}
		// Ignore unrelated ICMP traffic and keep waiting until the deadline.
	}
}

// tcpConnect is a shared TCP dial helper used by the ping fallback.
func (s *CheckService) tcpConnect(ctx context.Context, monitor *models.Monitor, address string, timeout time.Duration) (*models.Check, error) {
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	dialer := netguard.SafeDialer(timeout)
	start := time.Now()
	conn, err := dialer.DialContext(dialCtx, "tcp", address)
	elapsed := time.Since(start)
	if err != nil {
		if isTimeout(err) {
			return s.timeoutCheck(monitor, int(elapsed.Milliseconds()), err), nil
		}
		return s.failedCheck(monitor, elapsed, 0, err.Error()), nil
	}
	_ = conn.Close()
	s.logger.Printf("[ping] monitor=%s tcp-fallback addr=%s connected in %dms", monitor.ID, address, elapsed.Milliseconds())
	return s.successCheck(monitor, elapsed, 0), nil
}

// ExecuteDNSCheck resolves the monitor's domain and reports whether resolution
// succeeds within the timeout. On success the resolved addresses are recorded in
// the check's error_message field for informational purposes.
func (s *CheckService) ExecuteDNSCheck(ctx context.Context, monitor *models.Monitor) (*models.Check, error) {
	host := hostname(monitor.URL)

	return s.retryProbe(ctx, monitor, "dns", func(ctx context.Context, budget time.Duration) *models.Check {
		lookupCtx, cancel := context.WithTimeout(ctx, budget)
		defer cancel()

		start := time.Now()
		addrs, err := net.DefaultResolver.LookupHost(lookupCtx, host)
		elapsed := time.Since(start)
		if err != nil {
			if isTimeout(err) {
				s.logger.Printf("[dns] monitor=%s host=%s timeout", monitor.ID, host)
				return s.timeoutCheck(monitor, int(elapsed.Milliseconds()), err)
			}
			s.logger.Printf("[dns] monitor=%s host=%s error=%v", monitor.ID, host, err)
			return s.failedCheck(monitor, elapsed, 0, err.Error())
		}

		check := s.successCheck(monitor, elapsed, 0)
		check.ErrorMessage = fmt.Sprintf("resolved: %s", strings.Join(addrs, ", "))
		s.logger.Printf("[dns] monitor=%s host=%s resolved %d address(es) in %dms", monitor.ID, host, len(addrs), elapsed.Milliseconds())
		return check
	}), nil
}

// StoreCheck persists a check result for the given monitor, defaulting the
// timestamp to now if unset.
func (s *CheckService) StoreCheck(ctx context.Context, monitorID uuid.UUID, check *models.Check) error {
	check.MonitorID = monitorID
	if check.Timestamp.IsZero() {
		check.Timestamp = time.Now()
	}
	if err := s.db.WithContext(ctx).Create(check).Error; err != nil {
		return fmt.Errorf("storing check for monitor %s: %w", monitorID, err)
	}
	return nil
}

// GetRecentChecks returns up to limit most-recent checks for a monitor, newest
// first. A non-positive limit defaults to 100.
func (s *CheckService) GetRecentChecks(ctx context.Context, monitorID uuid.UUID, limit int) ([]models.Check, error) {
	if limit <= 0 {
		limit = 100
	}
	var checks []models.Check
	err := s.db.WithContext(ctx).
		Where("monitor_id = ?", monitorID).
		Order("timestamp DESC").
		Limit(limit).
		Find(&checks).Error
	if err != nil {
		return nil, fmt.Errorf("querying recent checks for monitor %s: %w", monitorID, err)
	}
	return checks, nil
}

// GetChecksInRange returns a monitor's checks whose timestamp falls within
// [start, end] (inclusive), newest first, applying limit and offset for
// pagination. A non-positive limit or offset is treated as unset. An empty
// result set is returned as an empty slice with a nil error.
func (s *CheckService) GetChecksInRange(ctx context.Context, monitorID uuid.UUID, start, end time.Time, limit, offset int) ([]models.Check, error) {
	query := s.db.WithContext(ctx).
		Where("monitor_id = ? AND timestamp >= ? AND timestamp <= ?", monitorID, start, end).
		Order("timestamp DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}

	var checks []models.Check
	if err := query.Find(&checks).Error; err != nil {
		return nil, fmt.Errorf("querying checks in range for monitor %s: %w", monitorID, err)
	}

	s.logger.Printf("[check] retrieved %d checks for monitor %s from %s to %s",
		len(checks), monitorID, start.Format(time.RFC3339), end.Format(time.RFC3339))
	return checks, nil
}

// CheckRangeSummary is the status breakdown and average response time over a
// range, computed by the database.
type CheckRangeSummary struct {
	Success int
	Failed  int
	Timeout int
	Total   int
	// AvgResponseMs averages successful checks only. A failed check records a
	// response time of zero, and counting those would report a latency nobody
	// experienced.
	AvgResponseMs int
}

// SummariseRange returns the status breakdown and average response time for a
// monitor over [start, end].
//
// Aggregated in SQL rather than by loading the rows. A report covering the
// full retention window on a monitor checked every minute spans well over a
// hundred thousand checks, and reading all of them into memory to produce four
// numbers is the expensive way to compute a sum. This returns one row.
func (s *CheckService) SummariseRange(ctx context.Context, monitorID uuid.UUID, start, end time.Time) (CheckRangeSummary, error) {
	var row struct {
		Success       int
		Failed        int
		Timeout       int
		Total         int
		AvgResponseMs float64
	}
	err := s.db.WithContext(ctx).Model(&models.Check{}).
		Select(`COUNT(*) FILTER (WHERE status = 'success') AS success,
		        COUNT(*) FILTER (WHERE status = 'failed')  AS failed,
		        COUNT(*) FILTER (WHERE status = 'timeout') AS timeout,
		        COUNT(*)                                   AS total,
		        COALESCE(AVG(response_time_ms) FILTER (WHERE status = 'success'), 0) AS avg_response_ms`).
		Where("monitor_id = ? AND timestamp >= ? AND timestamp <= ?", monitorID, start, end).
		Scan(&row).Error
	if err != nil {
		return CheckRangeSummary{}, fmt.Errorf("summarising checks for monitor %s: %w", monitorID, err)
	}
	return CheckRangeSummary{
		Success:       row.Success,
		Failed:        row.Failed,
		Timeout:       row.Timeout,
		Total:         row.Total,
		AvgResponseMs: int(row.AvgResponseMs),
	}, nil
}

// CountChecks returns the number of checks for a monitor whose timestamp falls
// within [start, end] (inclusive). Zero matches is reported as 0, not an error.
func (s *CheckService) CountChecks(ctx context.Context, monitorID uuid.UUID, start, end time.Time) (int64, error) {
	var count int64
	err := s.db.WithContext(ctx).
		Model(&models.Check{}).
		Where("monitor_id = ? AND timestamp >= ? AND timestamp <= ?", monitorID, start, end).
		Count(&count).Error
	if err != nil {
		return 0, fmt.Errorf("counting checks for monitor %s: %w", monitorID, err)
	}

	s.logger.Printf("[check] total checks for monitor %s in range: %d", monitorID, count)
	return count, nil
}

// checkTimeout returns the effective per-check timeout for a monitor.
// minProbeTimeout is the least time one probe is given, however the monitor's
// timeout divides.
const minProbeTimeout = time.Second

// probeAttempts is how many probes one check may send: the first, plus the
// monitor's configured retries.
func probeAttempts(m *models.Monitor) int {
	if m.Retries < 0 {
		return 1
	}
	return m.Retries + 1
}

// probeBudget divides a monitor's timeout across its attempts.
//
// Per-attempt rather than per-check, so retrying does not quietly multiply how
// long a check can take: timeout_seconds keeps meaning "how long this check may
// run". Without that, a monitor set to 10s with 3 retries would hold the
// sequential check loop for 40s on a host that is genuinely down, delaying
// every monitor behind it.
func probeBudget(total time.Duration, attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	per := total / time.Duration(attempts)
	if per < minProbeTimeout {
		per = minProbeTimeout
	}
	return per
}

// retryBackoff is the pause between probes.
//
// Short and near-constant, unlike the HTTP path's exponential seconds. The
// failures being retried here are lost packets and momentary congestion, which
// clear in milliseconds; waiting seconds would only make a check slower without
// making it more likely to succeed.
func retryBackoff(attempt int) time.Duration {
	return time.Duration(attempt) * 300 * time.Millisecond
}

func (s *CheckService) checkTimeout(m *models.Monitor) time.Duration {
	if m.TimeoutSeconds <= 0 {
		return defaultCheckTimeout
	}
	return time.Duration(m.TimeoutSeconds) * time.Second
}

func (s *CheckService) successCheck(m *models.Monitor, elapsed time.Duration, statusCode int) *models.Check {
	return &models.Check{
		MonitorID:      m.ID,
		Status:         checkSuccess,
		ResponseTimeMs: int(elapsed.Milliseconds()),
		StatusCode:     statusCode,
		Timestamp:      time.Now(),
	}
}

func (s *CheckService) failedCheck(m *models.Monitor, elapsed time.Duration, statusCode int, message string) *models.Check {
	return &models.Check{
		MonitorID:      m.ID,
		Status:         checkFailed,
		ResponseTimeMs: int(elapsed.Milliseconds()),
		StatusCode:     statusCode,
		ErrorMessage:   message,
		Timestamp:      time.Now(),
	}
}

func (s *CheckService) timeoutCheck(m *models.Monitor, ms int, err error) *models.Check {
	message := "operation timed out"
	if err != nil {
		message = err.Error()
	}
	return &models.Check{
		MonitorID:      m.ID,
		Status:         checkTimeout,
		ResponseTimeMs: ms,
		ErrorMessage:   message,
		Timestamp:      time.Now(),
	}
}

// isTimeout reports whether err represents a context deadline or a network
// timeout.
func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return false
}

// hostname extracts a bare hostname from a value that may be a full URL, a
// host:port pair, or a plain hostname.
func hostname(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.Contains(raw, "://") {
		if u, err := url.Parse(raw); err == nil && u.Host != "" {
			raw = u.Host
		}
	}
	if h, _, err := net.SplitHostPort(raw); err == nil {
		return h
	}
	return raw
}

// hostPort normalizes a value into a host:port address suitable for dialing. If
// no port is present, defaultPort is applied (or inferred from a URL scheme). An
// error is returned when no port can be determined.
func hostPort(raw, defaultPort string) (string, error) {
	raw = strings.TrimSpace(raw)
	scheme := ""
	if strings.Contains(raw, "://") {
		if u, err := url.Parse(raw); err == nil && u.Host != "" {
			raw = u.Host
			scheme = u.Scheme
		}
	}

	if _, _, err := net.SplitHostPort(raw); err == nil {
		return raw, nil
	}

	port := defaultPort
	switch scheme {
	case "https":
		port = "443"
	case "http":
		port = "80"
	}
	if port == "" {
		return "", fmt.Errorf("no port specified in %q", raw)
	}
	return net.JoinHostPort(raw, port), nil
}
