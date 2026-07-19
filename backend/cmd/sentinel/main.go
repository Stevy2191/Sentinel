// Command sentinel starts the Sentinel uptime monitoring server: it connects to
// the database, runs migrations, wires up services and the REST API, launches
// the monitoring loop, and serves HTTP with graceful shutdown.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
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
	Port          string
	Environment   string
	CheckInterval time.Duration
	MigrationsDir string
}

func loadConfig() config {
	return config{
		Port:          getenv("PORT", "3000"),
		Environment:   getenv("ENVIRONMENT", "development"),
		CheckInterval: time.Duration(getenvInt("DEFAULT_CHECK_INTERVAL", 30)) * time.Second,
		MigrationsDir: getenv("MIGRATIONS_DIR", "migrations"),
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

	// 4. Notification plugins (each is optional; unconfigured channels are skipped).
	registerNotificationPlugins(notificationManager)

	// 5. HTTP router + routes.
	if cfg.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":    "healthy",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
	})
	v1 := router.Group("/api/v1")
	api.RegisterMonitorRoutes(v1, monitorService, checkService)
	api.RegisterCheckRoutes(v1, checkService, incidentService, monitorService)
	api.RegisterReportRoutes(v1, monitorService, checkService, incidentService)
	api.RegisterStatusPageRoutes(router, statusPageService, incidentService)

	// 6. Monitoring loop.
	loopCtx, cancelLoop := context.WithCancel(context.Background())
	go StartMonitoringLoop(loopCtx, db, monitorService, checkService, incidentService, notificationManager, cfg.CheckInterval)

	// 7. HTTP server.
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

	// 8. Graceful shutdown.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		cancelLoop()
		return fmt.Errorf("http server: %w", err)
	case sig := <-quit:
		log.Printf("shutdown signal received: %s", sig)
	}

	// Stop the monitoring loop, then the HTTP server, then the database.
	cancelLoop()

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

	message := &notifications.NotificationMessage{
		MonitorID:      monitor.ID,
		MonitorName:    monitor.Name,
		MonitorURL:     monitor.URL,
		PreviousStatus: previous,
		Timestamp:      time.Now(),
		ResponseTimeMs: check.ResponseTimeMs,
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

func getenvInt(key string, fallback int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
