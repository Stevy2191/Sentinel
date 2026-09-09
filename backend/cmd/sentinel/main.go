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
	"github.com/google/uuid"
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
	AppName             string
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
		// Instance display name. Seeded into the settings table on first run;
		// after that an admin edits it in Settings -> System.
		AppName: getenv("APP_NAME", ""),
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
	settingsService := services.NewSettingsService(db)
	// Resolved at each use, not captured here, so an admin editing the base URL
	// in Settings -> System changes the next email rather than needing a restart.
	// Falls back to SENTINEL_BASE_URL inside each consumer when unset.
	resolveBaseURL := services.BaseURLFunc(func() string {
		return settingsService.BaseURL(context.Background())
	})
	// The notifications package cannot import services (services imports it),
	// so the lookup is injected rather than called directly.
	notifications.SetBaseURLResolver(resolveBaseURL)
	invitationService := services.NewInvitationService(db, authService, resolveBaseURL)
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
	reportMailer := services.NewReportMailer(db, resolveBaseURL)
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
	// The system settings follow the same seed-once rule: the environment
	// provides the initial value, and from then on the stored value wins so an
	// admin's edit in Settings -> System survives a restart. An unset env var
	// seeds nothing, leaving the code default in force.
	if _, err := settingsService.SeedString(settingsCtx, models.SettingAppName, cfg.AppName); err != nil {
		return fmt.Errorf("seeding app name: %w", err)
	}
	if _, err := settingsService.SeedString(settingsCtx, models.SettingBaseURL, cfg.BaseURL); err != nil {
		return fmt.Errorf("seeding base URL: %w", err)
	}
	if _, err := settingsService.SeedInt(settingsCtx, models.SettingDefaultCheckInterval,
		models.DefaultMonitorCheckInterval); err != nil {
		return fmt.Errorf("seeding default check interval: %w", err)
	}
	if _, err := settingsService.SeedInt(settingsCtx, models.SettingCheckRetentionDays,
		models.DefaultCheckRetentionDays); err != nil {
		log.Fatalf("seeding check retention setting: %v", err)
	}
	if _, err := settingsService.SeedInt(settingsCtx, models.SettingIncidentRetentionDays,
		models.DefaultIncidentRetentionDays); err != nil {
		return fmt.Errorf("seeding incident retention: %w", err)
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
				log.Printf("  To change it: Users -> User Registration in the web UI,")
				log.Printf("  or: UPDATE settings SET value='%t', updated_at=now() WHERE key='%s';",
					cfg.RegistrationEnabled, models.SettingRegistrationEnabled)
			}
		}
	}

	// 4. Notification channels. The database is the only source; anything
	// configured through the environment is imported into it on first run and
	// is an ordinary channel from then on.
	notifyCtx := context.Background()
	if err := importEnvNotificationChannels(notifyCtx, db); err != nil {
		// Not fatal: an install with channels already in the database does not
		// need the import, and failing to start over it would be worse than
		// starting without one env-configured channel.
		log.Printf("warning: importing notification channels from the environment: %v", err)
	}
	notificationConfigService := services.NewNotificationConfigService(db, notificationManager)
	sslChecker := services.NewSSLCheckerService(db, notificationManager)
	incidentRetention := services.NewIncidentRetentionService(db, settingsService)
	if err := notificationManager.LoadFromDatabase(notifyCtx); err != nil {
		log.Printf("warning: loading notification configs from database: %v", err)
	}
	// Monitors selected channels by type before an install could hold several
	// of one type. Rewrite those selections to ids now that the channels exist,
	// so the stored value means one specific channel.
	if err := normalizeMonitorNotifyChannels(notifyCtx, db); err != nil {
		log.Printf("warning: normalising monitor notification selections: %v", err)
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
	api.RegisterMonitorRoutes(v1, monitorService, checkService, settingsService)
	api.RegisterMonitorCreationRoutes(v1, monitorService, checkService)
	api.RegisterDiscoveryRoutes(v1, discoveryService, authService)
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
	api.RegisterSettingsRoutes(v1, settingsService, models.DefaultMonitorCheckInterval, authService)
	api.RegisterSSLCertificateRoutes(v1, sslChecker, authService)
	// Per-user theme (not admin-gated): only AuthMiddleware applies.
	// Self password change (any authenticated user).
	v1.POST("/auth/change-password", api.ChangeOwnPasswordHandler(authService))

	// Admin-only user management + invitations (admin invitation routes here,
	// public accept/details routes registered on the router).
	admin := v1.Group("")
	admin.Use(api.RequireAdmin(authService))
	api.RegisterUserManagementRoutes(admin, authService)
	api.RegisterAuditRoutes(admin, auditService)
	api.RegisterInvitationRoutes(admin, router, invitationService, authService)
	api.RegisterNotificationConfigRoutes(v1, notificationConfigService, authService)

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
	go StartSSLCheckLoop(loopCtx, sslChecker)
	go incidentRetention.StartPurgeLoop(loopCtx)

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

// importEnvNotificationChannels seeds the notification_configs table from the
// environment, once, for any channel type that has no row yet.
//
// Channels used to be registered straight from the environment, living entirely
// outside the database. That stopped working when monitors began selecting
// channels by id: an env-only channel has no id to select. Rather than keep two
// kinds of channel, an env-configured one is imported into the table on first
// run and is thereafter an ordinary channel — editable, disableable, deletable.
//
// This follows the same seed-once rule as app_name and base_url: the row wins
// from then on, so an admin's edit is not overwritten by the next restart, and
// deleting an imported channel does not resurrect it while the variable is
// still set.
func importEnvNotificationChannels(ctx context.Context, db *gorm.DB) error {
	// Each entry builds a config from the environment. A nil return means the
	// variables for that channel are not set, so there is nothing to import.
	builders := []struct {
		channel string
		label   string
		build   func() *models.NotificationConfig
	}{
		{"email", "Email", envEmailConfig},
		{"slack", "Slack", envURLConfig("slack", "SLACK_WEBHOOK_URL")},
		{"discord", "Discord", envURLConfig("discord", "DISCORD_WEBHOOK_URL")},
		{"webhook", "Webhook", envURLConfig("webhook", "WEBHOOK_URL")},
		{"telegram", "Telegram", envTelegramConfig},
		{"ntfy", "Ntfy", envNtfyConfig},
	}

	for _, b := range builders {
		var count int64
		if err := db.WithContext(ctx).Model(&models.NotificationConfig{}).
			Where("channel = ?", b.channel).Count(&count).Error; err != nil {
			return fmt.Errorf("checking existing %s channel: %w", b.channel, err)
		}
		if count > 0 {
			continue // already managed in the database; the environment no longer applies
		}
		cfg := b.build()
		if cfg == nil {
			continue // not configured in the environment
		}
		cfg.ID = uuid.New()
		cfg.Name = b.label + " (from env)"
		cfg.Enabled = true
		now := time.Now()
		cfg.CreatedAt = now
		cfg.UpdatedAt = now
		if err := db.WithContext(ctx).Create(cfg).Error; err != nil {
			return fmt.Errorf("importing %s channel from environment: %w", b.channel, err)
		}
		log.Printf("[notify] imported %s channel from the environment as %q", b.channel, cfg.Name)
	}
	return nil
}

// normalizeMonitorNotifyChannels rewrites channel *type* entries in every
// monitor's notify_channels into the ids of the channels of that type.
//
// Selections were stored as type names ("slack") when a type could only have
// one channel. Leaving them that way would work — dispatch still matches on
// type as a fallback — but it would silently mean "every Slack channel", which
// is not a choice anyone made. Rewriting once makes the stored value say what
// it means.
//
// Idempotent: entries that are already ids are left alone, and an entry naming
// a type with no channels is kept rather than dropped, so a selection is never
// silently discarded because a channel is temporarily absent.
func normalizeMonitorNotifyChannels(ctx context.Context, db *gorm.DB) error {
	var configs []models.NotificationConfig
	if err := db.WithContext(ctx).Find(&configs).Error; err != nil {
		return fmt.Errorf("loading channels: %w", err)
	}
	byType := map[string][]string{}
	for _, cfg := range configs {
		byType[cfg.Channel] = append(byType[cfg.Channel], cfg.ID.String())
	}
	if len(byType) == 0 {
		return nil
	}

	var monitors []models.Monitor
	if err := db.WithContext(ctx).Find(&monitors).Error; err != nil {
		return fmt.Errorf("loading monitors: %w", err)
	}

	converted := 0
	for i := range monitors {
		m := &monitors[i]
		// nil means "every channel" and [] means "none"; neither names anything
		// to rewrite.
		if len(m.NotifyChannels) == 0 {
			continue
		}
		next := make([]string, 0, len(m.NotifyChannels))
		changed := false
		seen := map[string]bool{}
		for _, entry := range m.NotifyChannels {
			if _, err := uuid.Parse(entry); err == nil {
				if !seen[entry] {
					seen[entry] = true
					next = append(next, entry)
				}
				continue
			}
			ids, ok := byType[entry]
			if !ok {
				if !seen[entry] {
					seen[entry] = true
					next = append(next, entry)
				}
				continue
			}
			changed = true
			for _, id := range ids {
				if !seen[id] {
					seen[id] = true
					next = append(next, id)
				}
			}
		}
		if !changed {
			continue
		}
		if err := db.WithContext(ctx).Model(&models.Monitor{}).Where("id = ?", m.ID).
			Update("notify_channels", models.StringSlice(next)).Error; err != nil {
			return fmt.Errorf("updating monitor %s: %w", m.ID, err)
		}
		converted++
	}
	if converted > 0 {
		log.Printf("[notify] rewrote channel selections on %d monitor(s) from type names to ids", converted)
	}
	return nil
}

// strPtr returns a pointer to s, or nil when s is empty after trimming.
func strPtr(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

func envEmailConfig() *models.NotificationConfig {
	host := strPtr(os.Getenv("SMTP_HOST"))
	if host == nil {
		return nil
	}
	port := getenvInt("SMTP_PORT", 587)
	cfg := &models.NotificationConfig{
		Channel:      "email",
		SMTPHost:     host,
		SMTPPort:     &port,
		SMTPUser:     strPtr(os.Getenv("SMTP_USER")),
		SMTPPassword: strPtr(os.Getenv("SMTP_PASSWORD")),
		SMTPFrom:     strPtr(os.Getenv("SMTP_FROM")),
		SMTPSecurity: strPtr(os.Getenv("SMTP_SECURITY")),
	}
	cfg.SMTPSkipTLSVerify = getenvBool("SMTP_SKIP_TLS_VERIFY", false)
	return cfg
}

// envURLConfig builds a webhook-style channel from a single URL variable.
func envURLConfig(channel, envVar string) func() *models.NotificationConfig {
	return func() *models.NotificationConfig {
		url := strPtr(os.Getenv(envVar))
		if url == nil {
			return nil
		}
		return &models.NotificationConfig{Channel: channel, WebhookURL: url}
	}
}

func envTelegramConfig() *models.NotificationConfig {
	token := strPtr(os.Getenv("TELEGRAM_BOT_TOKEN"))
	chat := strPtr(os.Getenv("TELEGRAM_CHAT_ID"))
	if token == nil || chat == nil {
		return nil
	}
	return &models.NotificationConfig{Channel: "telegram", TelegramBotToken: token, TelegramChatID: chat}
}

func envNtfyConfig() *models.NotificationConfig {
	topic := strPtr(os.Getenv("NTFY_TOPIC"))
	if topic == nil {
		return nil
	}
	url := strPtr(os.Getenv("NTFY_URL"))
	if url == nil {
		def := "https://ntfy.sh"
		url = &def
	}
	return &models.NotificationConfig{
		Channel:       "ntfy",
		NtfyURL:       url,
		NtfyTopic:     topic,
		NtfyAuthToken: strPtr(os.Getenv("NTFY_AUTH_TOKEN")),
	}
}

// StartSSLCheckLoop re-reads every watched certificate once a day.
//
// It sweeps shortly after startup as well as on the daily tick. An expiry date
// only moves once a day, but a process that has been down for a week would
// otherwise wait a further day before noticing a certificate expired while it
// was off — and the first sweep is also what fills in rows whose initial check
// failed.
func StartSSLCheckLoop(ctx context.Context, checker *services.SSLCheckerService) {
	const interval = models.SSLCheckIntervalSeconds * time.Second

	// Long enough after boot that the first sweep does not compete with
	// migrations and the initial monitor pass for the network.
	select {
	case <-ctx.Done():
		return
	case <-time.After(30 * time.Second):
	}

	sweep := func() {
		// Bounded independently of the loop: a sweep that hangs on one slow
		// host must not delay tomorrow's.
		sweepCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
		defer cancel()
		checked, failed, err := checker.CheckAll(sweepCtx)
		if err != nil {
			log.Printf("[ssl] sweep failed: %v", err)
			return
		}
		if checked > 0 {
			log.Printf("[ssl] checked %d certificate(s), %d could not be read", checked, failed)
		}
	}
	sweep()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	log.Printf("[ssl] certificate check loop started (interval=%s)", interval)
	for {
		select {
		case <-ctx.Done():
			log.Println("[ssl] certificate check loop stopped")
			return
		case <-ticker.C:
			sweep()
		}
	}
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
		// Newly offline: open an incident and alert. The failing check's own
		// status and message are carried across so the incident says what
		// happened rather than only that something did.
		if incident, err := incidentService.CreateIncidentFromCheck(
			ctx, monitor.ID, time.Now(),
			models.IncidentTypeForCheck(check.Status), check.ErrorMessage,
		); err != nil {
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
