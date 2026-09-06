// Command sentinel starts the Sentinel uptime monitoring server: it connects to
// the database, runs migrations, wires up services and the REST API, launches
// the monitoring loop, and serves HTTP with graceful shutdown.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/api"
	"github.com/Stevy2191/Sentinel/backend/internal/database"
	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/notifications"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

const shutdownTimeout = 30 * time.Second

// config holds runtime configuration read from the environment.
type config struct {
	Port                string
	Environment         string
	CheckInterval       time.Duration
	MigrationsDir       string
	ReportsDir          string
	ReportWorkers       int
	BaseURL             string
	RegistrationEnabled bool
}

func loadConfig() config {
	return config{
		Port:          getenv("PORT", "3000"),
		Environment:   getenv("ENVIRONMENT", "development"),
		CheckInterval: time.Duration(getenvInt("DEFAULT_CHECK_INTERVAL", 30)) * time.Second,
		MigrationsDir: getenv("MIGRATIONS_DIR", "migrations"),
		// Where generated report PDFs are written. Mount this as a volume, or
		// generated reports vanish when the container is replaced.
		ReportsDir: getenv("REPORTS_DIR", "reports"),
		// Absolute base URL used to build links in outgoing report email.
		BaseURL: getenv("SENTINEL_BASE_URL", ""),
		// How many reports may render concurrently. Each holds a PDF in memory,
		// so this is deliberately small.
		ReportWorkers: getenvInt("REPORT_WORKERS", 2),
		// Closed by default for security; the first account can always be created
		// (see RegisterHandler), and an admin can open registration at runtime.
		RegistrationEnabled: getenvBool("REGISTRATION_ENABLED", false),
	}
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[sentinel] ")

	if err := run(); err != nil {
		log.Fatalf("fatal: %v", err)
	}
}

// run wires the application together and blocks until shutdown.
func run() error {
	cfg := loadConfig()
	log.Printf("starting Sentinel (env=%s)", cfg.Environment)

	// 1. Database.
	db, err := database.NewDB(nil)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}

	// 2. Migrations.
	if err := runMigrations(db, cfg.MigrationsDir); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	// 3. Services.
	monitorService := services.NewMonitorService(db)
	checkService := services.NewCheckService(db)
	incidentService := services.NewIncidentService(db)
	statusPageService := services.NewStatusPageService(db)
	notificationManager := notifications.NewNotificationManager(db)
	authService := services.NewAuthService(db, resolveJWTSecret())
	invitationService := services.NewInvitationService(db, authService)
	settingsService := services.NewSettingsService(db)
	discoveryService := services.NewDiscoveryService()
	reportAggregator := services.NewReportAggregatorService(db)
	auditService := services.NewAuditService(db)
	pdfRenderer, err := services.NewPDFRendererService(cfg.ReportsDir)
	if err != nil {
		return fmt.Errorf("initializing report renderer: %w", err)
	}
	reportBuilder := api.NewReportBuilder(db, reportAggregator, pdfRenderer, nil)
	// PDF rendering runs on a worker pool rather than in the request handler.
	reportJobs := services.NewReportJobQueue(db, services.NewReportGenerator(db, reportAggregator, pdfRenderer), cfg.ReportWorkers)
	reportBuilder.SetJobQueue(reportJobs)
	reportBuilder.SetAudit(auditService)
	reportGenerator := services.NewReportGenerator(db, reportAggregator, pdfRenderer)
	// Scheduled delivery sends through the same SMTP configuration as the email
	// notification channel, so it inherits its connection-security settings.
	reportMailer := services.NewReportMailer(db, cfg.BaseURL)
	reportScheduler := services.NewReportSchedulerService(db, reportGenerator, reportMailer)
	// Wired after construction: deleting a report must also stop its cron jobs.
	reportBuilder.SetScheduler(reportScheduler)

	// Seed the registration setting from the environment on first run only; once
	// stored, an admin's runtime change is authoritative across restarts.
	settingsCtx := context.Background()
	seeded, err := settingsService.SeedBool(settingsCtx, models.SettingRegistrationEnabled, cfg.RegistrationEnabled)
	if err != nil {
		return fmt.Errorf("seeding settings: %w", err)
	}
	// Say so when the environment disagrees with what is stored. The stored
	// value winning is deliberate - it is what lets an admin close registration
	// from the UI and have it stick across restarts - but silently ignoring an
	// explicitly set REGISTRATION_ENABLED looks exactly like the setting not
	// working, with nothing in the log to explain it.
	if !seeded {
		if _, explicit := os.LookupEnv("REGISTRATION_ENABLED"); explicit {
			if stored := settingsService.RegistrationEnabled(settingsCtx); stored != cfg.RegistrationEnabled {
				log.Printf("WARNING: REGISTRATION_ENABLED=%t is being IGNORED. Self-registration is %s, "+
					"because the stored setting takes precedence after the first run.",
					cfg.RegistrationEnabled, map[bool]string{true: "OPEN", false: "CLOSED"}[stored])
				log.Printf("  To change it: Settings -> Security -> User Registration in the web UI,")
				log.Printf("  or: UPDATE settings SET value='%t', updated_at=now() WHERE key='%s';",
					cfg.RegistrationEnabled, models.SettingRegistrationEnabled)
			}
		}
	}

	// 4. Notification plugins. Env-configured channels register first (backward
	// compatible); then database-backed channel configs are loaded and take
	// precedence over env for the same channel.
	registerNotificationPlugins(notificationManager)
	notificationConfigService := services.NewNotificationConfigService(db, notificationManager)
	if err := notificationManager.LoadFromDatabase(context.Background()); err != nil {
		log.Printf("warning: loading notification configs from database: %v", err)
	}

	// 5. HTTP router + routes.
	if cfg.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.New()
	if err := router.SetTrustedProxies(resolveTrustedProxies()); err != nil {
		return fmt.Errorf("setting trusted proxies: %w", err)
	}
	router.Use(gin.Logger(), gin.Recovery())
	router.Use(api.SecurityHeaders())

	// Liveness probe. Registered at both paths on purpose: /health is what the
	// container healthcheck hits directly, and /api/health is reachable through
	// the nginx /api/ proxy, so the frontend (or an external monitor) can check
	// the backend without a second published port.
	health := func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":    "healthy",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
	}
	router.GET("/health", health)
	router.GET("/api/health", health)
	// Public auth endpoints (register/login/mfa-verify) + public status pages.
	api.RegisterAuthRoutes(router, authService, settingsService)
	api.RegisterPublicStatusRoutes(router, statusPageService, incidentService)
	// Share-token report access: public by design, so outside the authenticated
	// v1 group (same split as the public status pages above).
	api.RegisterPublicReportRoutes(router, reportBuilder)

	// All other /api/v1 routes require a valid JWT.
	v1 := router.Group("/api/v1")
	v1.Use(api.AuthMiddleware(authService))
	api.RegisterMonitorRoutes(v1, monitorService, checkService)
	api.RegisterMonitorCreationRoutes(v1, monitorService, checkService)
	api.RegisterDiscoveryRoutes(v1, discoveryService)
	api.RegisterCheckRoutes(v1, checkService, incidentService, monitorService)
	api.RegisterReportRoutes(v1, monitorService, checkService, incidentService)
	// Saved-report builder: definitions, PDF generation, history, sharing.
	api.RegisterReportBuilderRoutes(v1, reportBuilder)
	api.RegisterReportScheduleRoutes(v1, api.NewReportScheduleHandler(db, reportScheduler, auditService))
	api.RegisterIncidentRoutes(v1, incidentService, monitorService, db)
	api.RegisterMonitorGroupRoutes(v1, monitorService, incidentService)
	api.RegisterMonitorSharingRoutes(v1, monitorService, authService)
	api.RegisterStatusPageRoutes(v1, statusPageService, incidentService)
	api.RegisterNotificationRoutes(v1, notificationManager, monitorService)
	api.RegisterSettingsRoutes(v1, settingsService)
	// Per-user theme (not admin-gated): only AuthMiddleware applies.
	v1.PATCH("/settings/theme", api.UpdateUserThemeHandler(authService))
	// Self password change (any authenticated user).
	v1.POST("/auth/change-password", api.ChangeOwnPasswordHandler(authService))

	// Admin-only user management + invitations (admin invitation routes here,
	// public accept/details routes registered on the router).
	admin := v1.Group("")
	admin.Use(api.RequireAdmin())
	api.RegisterUserManagementRoutes(admin, authService)
	api.RegisterAuditRoutes(admin, auditService)
	api.RegisterInvitationRoutes(admin, router, invitationService, authService)
	api.RegisterNotificationConfigRoutes(v1, notificationConfigService)

	// 6. Report render workers.
	reportJobs.Start(context.Background())

	// 7. Scheduled report delivery. A failure to load schedules must not stop
	// the server: monitoring is the primary job and continues without them.
	if err := reportScheduler.Start(context.Background()); err != nil {
		log.Printf("warning: report scheduler not started: %v", err)
	}

	// 8. Monitoring loop.
	loopCtx, cancelLoop := context.WithCancel(context.Background())
	go StartMonitoringLoop(loopCtx, db, monitorService, checkService, incidentService, notificationManager, cfg.CheckInterval)

	// 9. HTTP server.
	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}
	serverErr := make(chan error, 1)
	go func() {
		log.Printf("HTTP server listening on %s", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// 10. Graceful shutdown.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		cancelLoop()
		reportScheduler.Stop()
		reportJobs.Stop()
		return fmt.Errorf("http server: %w", err)
	case sig := <-quit:
		log.Printf("shutdown signal received: %s", sig)
	}

	// Stop the monitoring loop and the report scheduler, then the HTTP server,
	// then the database.
	cancelLoop()
	reportScheduler.Stop()
	// Waits for the in-flight render to finish; anything still queued is picked
	// up by the next process, since Start requeues interrupted jobs.
	reportJobs.Stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("http shutdown error: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}

	log.Println("stopped cleanly")
	return nil
}

// registerNotificationPlugins builds each plugin from the environment and
// registers those that are configured. A plugin whose environment is not set
// returns an error from its constructor and is skipped rather than fatal.
func registerNotificationPlugins(manager *notifications.NotificationManager) {
	register := func(name string, plugin notifications.NotificationPlugin, err error) {
		if err != nil {
			log.Printf("notification plugin %q not configured: %v", name, err)
			return
		}
		if err := manager.RegisterPlugin(plugin); err != nil {
			log.Printf("failed to register %q plugin: %v", name, err)
			return
		}
		log.Printf("%s notification plugin registered", name)
	}

	email, err := notifications.NewEmailPlugin()
	register("email", email, err)
	ntfy, err := notifications.NewNtfyPlugin()
	register("ntfy", ntfy, err)
	slack, err := notifications.NewSlackPlugin()
	register("slack", slack, err)
	discord, err := notifications.NewDiscordPlugin()
	register("discord", discord, err)
	telegram, err := notifications.NewTelegramPlugin()
	register("telegram", telegram, err)
	webhook, err := notifications.NewWebhookPlugin()
	register("webhook", webhook, err)
}

// runMigrations applies any *.sql files in dir that have not yet been recorded
// in the schema_migrations table, in filename order.
func runMigrations(db *gorm.DB, dir string) error {
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		filename   TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`).Error; err != nil {
		return fmt.Errorf("creating schema_migrations table: %w", err)
	}

	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		return fmt.Errorf("listing migrations in %q: %w", dir, err)
	}
	sort.Strings(files)

	for _, path := range files {
		name := filepath.Base(path)

		var applied int64
		if err := db.Raw("SELECT count(*) FROM schema_migrations WHERE filename = ?", name).Scan(&applied).Error; err != nil {
			return fmt.Errorf("checking migration %q: %w", name, err)
		}
		if applied > 0 {
			continue
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading migration %q: %w", name, err)
		}
		log.Printf("applying migration %s", name)
		if err := db.Exec(string(content)).Error; err != nil {
			return fmt.Errorf("applying migration %q: %w", name, err)
		}
		if err := db.Exec("INSERT INTO schema_migrations (filename) VALUES (?)", name).Error; err != nil {
			return fmt.Errorf("recording migration %q: %w", name, err)
		}
	}
	return nil
}

// StartMonitoringLoop periodically checks all enabled monitors until the context
// is cancelled.
func StartMonitoringLoop(
	ctx context.Context,
	db *gorm.DB,
	monitorService *services.MonitorService,
	checkService *services.CheckService,
	incidentService *services.IncidentService,
	notificationManager *notifications.NotificationManager,
	interval time.Duration,
) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("monitoring loop started (interval=%s)", interval)
	for {
		select {
		case <-ctx.Done():
			log.Println("monitoring loop stopped")
			return
		case <-ticker.C:
			runMonitoringCycle(ctx, db, monitorService, checkService, incidentService, notificationManager)
		}
	}
}

// runMonitoringCycle checks every enabled monitor once. It recovers from panics
// so a single bad cycle cannot crash the loop.
func runMonitoringCycle(
	ctx context.Context,
	db *gorm.DB,
	monitorService *services.MonitorService,
	checkService *services.CheckService,
	incidentService *services.IncidentService,
	notificationManager *notifications.NotificationManager,
) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("monitoring cycle panic recovered: %v", r)
		}
	}()

	monitors, err := monitorService.ListMonitors(ctx, map[string]interface{}{"enabled": true})
	if err != nil {
		log.Printf("monitoring cycle: listing monitors: %v", err)
		return
	}

	failures := 0
	for i := range monitors {
		monitor := monitors[i]

		check, err := checkService.ExecuteCheck(ctx, &monitor)
		if err != nil {
			log.Printf("monitoring cycle: check monitor %s: %v", monitor.ID, err)
			failures++
			continue
		}
		if err := checkService.StoreCheck(ctx, monitor.ID, check); err != nil {
			log.Printf("monitoring cycle: store check for %s: %v", monitor.ID, err)
		}

		newStatus := models.StatusOnline
		if check.Status != "success" {
			newStatus = models.StatusOffline
			failures++
		}

		handleStatusChange(ctx, incidentService, notificationManager, &monitor, check, newStatus)

		// Persist the latest status snapshot on the monitor row.
		if err := db.WithContext(ctx).Model(&models.Monitor{}).
			Where("id = ?", monitor.ID).
			Updates(map[string]interface{}{
				"current_status":        newStatus,
				"last_check_at":         time.Now(),
				"last_response_time_ms": check.ResponseTimeMs,
			}).Error; err != nil {
			log.Printf("monitoring cycle: update monitor %s: %v", monitor.ID, err)
		}
	}

	log.Printf("%d monitors checked, %d failed", len(monitors), failures)
}

// handleStatusChange opens/closes incidents and sends notifications when a
// monitor transitions between online and offline.
func handleStatusChange(
	ctx context.Context,
	incidentService *services.IncidentService,
	notificationManager *notifications.NotificationManager,
	monitor *models.Monitor,
	check *models.Check,
	newStatus string,
) {
	previous := monitor.CurrentStatus
	if newStatus == previous {
		return
	}

	// During a maintenance window, record checks but suppress incidents and
	// notifications entirely.
	if monitor.IsInMaintenanceWindow(time.Now()) {
		log.Printf("monitor %s changed state but is in maintenance; skipping incident/notification", monitor.ID)
		return
	}

	// A monitor opted out of notifications still opens and closes incidents —
	// the history stays accurate, only the alerting is silenced.
	if !monitor.NotifiesAnyChannel() {
		log.Printf("monitor %s changed state but has notifications disabled; recording only", monitor.ID)
	}

	message := &notifications.NotificationMessage{
		MonitorID:      monitor.ID,
		MonitorName:    monitor.Name,
		MonitorURL:     monitor.URL,
		PreviousStatus: previous,
		Timestamp:      time.Now(),
		ResponseTimeMs: check.ResponseTimeMs,
		Channels:       monitor.NotifyChannels,
	}

	switch {
	case newStatus == models.StatusOffline && previous != models.StatusOffline:
		// Newly offline: open an incident and alert.
		if incident, err := incidentService.CreateIncident(ctx, monitor.ID, time.Now()); err != nil {
			log.Printf("opening incident for %s: %v", monitor.ID, err)
		} else {
			message.IncidentID = &incident.ID
		}
		message.Status = "down"
		if err := notificationManager.SendNotification(ctx, message); err != nil {
			log.Printf("sending down notification for %s: %v", monitor.ID, err)
		}

	case newStatus == models.StatusOnline && previous == models.StatusOffline:
		// Recovered: close the active incident and alert.
		if active, err := incidentService.GetActiveIncident(ctx, monitor.ID); err == nil && active != nil {
			if closed, err := incidentService.CloseIncident(ctx, active.ID, time.Now()); err != nil {
				log.Printf("closing incident for %s: %v", monitor.ID, err)
			} else {
				message.IncidentID = &closed.ID
				message.DowntimeDuration = time.Duration(closed.DurationSeconds) * time.Second
			}
		}
		message.Status = "recovered"
		if err := notificationManager.SendNotification(ctx, message); err != nil {
			log.Printf("sending recovery notification for %s: %v", monitor.ID, err)
		}
	}
	// Other transitions (e.g. unknown->online on first check) update status only.
}

func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

// resolveJWTSecret returns JWT_SECRET, or a random per-process secret (with a
// warning) if it is unset so the server still runs in development.
func resolveJWTSecret() string {
	if s := os.Getenv("JWT_SECRET"); s != "" {
		return s
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing means the OS's secure entropy source is broken.
		// There is no safe fallback for a JWT signing secret in that state -
		// continuing with a hardcoded, publicly-known string would let anyone
		// forge admin tokens. Refuse to start instead.
		log.Fatalf("FATAL: could not generate a random JWT secret (crypto/rand failed: %v). "+
			"Set JWT_SECRET explicitly and restart.", err)
	}
	log.Printf("WARNING: JWT_SECRET is not set; using a random per-process secret (all sessions reset on restart). Set JWT_SECRET in production.")
	return base64.StdEncoding.EncodeToString(b)
}

// resolveTrustedProxies returns the CIDR ranges Gin should trust
// X-Forwarded-For/X-Real-IP from - i.e. the reverse proxy (nginx) sitting in
// front of this service, not arbitrary clients. Without this, Gin's default
// is to trust every peer, which makes c.ClientIP() spoofable via a
// client-supplied X-Forwarded-For header - harmless today (it only affects
// log lines) but would become a real bypass if IP-based rate limiting is
// ever added without this fixed first. Defaults to the private ranges Docker
// commonly assigns to bridge networks; set TRUSTED_PROXIES (comma-separated
// CIDRs) to override if you front Sentinel with a different reverse proxy.
func resolveTrustedProxies() []string {
	if v := os.Getenv("TRUSTED_PROXIES"); v != "" {
		var out []string
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	return []string{"172.16.0.0/12", "192.168.0.0/16", "10.0.0.0/8"}
}

func getenvInt(key string, fallback int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getenvBool(key string, fallback bool) bool {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}
