// Command sentinel starts the Sentinel uptime monitoring HTTP server.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

const (
	defaultPort         = "3000"
	defaultEnvironment  = "development"
	shutdownGracePeriod = 10 * time.Second
	readHeaderTimeout   = 5 * time.Second
)

func main() {
	logger := newLogger()
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("server terminated with error", slog.Any("error", err))
		os.Exit(1)
	}
}

// run wires up configuration, the HTTP server, and graceful shutdown. It returns
// an error rather than calling os.Exit so that deferred cleanup runs correctly.
func run(logger *slog.Logger) error {
	// Load .env if present. A missing file is not fatal: in production,
	// configuration typically comes from real environment variables.
	if err := godotenv.Load(); err != nil {
		logger.Info("no .env file loaded; relying on process environment", slog.Any("reason", err))
	}

	environment := getenv("ENVIRONMENT", defaultEnvironment)
	port := getenv("PORT", defaultPort)

	if environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := newRouter()

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           router,
		ReadHeaderTimeout: readHeaderTimeout,
	}

	// Start the server in a goroutine so that it does not block graceful
	// shutdown handling below.
	serverErr := make(chan error, 1)
	go func() {
		logger.Info("starting Sentinel server",
			slog.String("environment", environment),
			slog.String("addr", srv.Addr),
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	// Wait for either a fatal server error or an OS interrupt/termination signal.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		return err
	case sig := <-quit:
		logger.Info("shutdown signal received", slog.String("signal", sig.String()))
	}

	// Attempt a graceful shutdown, allowing in-flight requests to complete.
	ctx, cancel := context.WithTimeout(context.Background(), shutdownGracePeriod)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		return err
	}

	logger.Info("server stopped cleanly")
	return nil
}

// newRouter builds the Gin engine and registers baseline routes.
func newRouter() *gin.Engine {
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	return router
}

// newLogger returns a structured JSON logger writing to stdout.
func newLogger() *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	return slog.New(handler)
}

// getenv returns the value of the environment variable named by key, or
// fallback if the variable is unset or empty.
func getenv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}
