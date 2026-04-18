package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/Shisa-Fosho/services/internal/shared/envutil"
	sharednats "github.com/Shisa-Fosho/services/internal/shared/nats"
	"github.com/Shisa-Fosho/services/internal/shared/observability"
	"github.com/Shisa-Fosho/services/internal/shared/postgres"
)

const serviceName = "indexer"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", serviceName, err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Observability.
	logger, err := observability.NewLogger(serviceName)
	if err != nil {
		return fmt.Errorf("creating logger: %w", err)
	}
	defer logger.Sync()

	metrics := observability.NewMetrics(serviceName)

	tracerShutdown, err := observability.TracerFromEnv(ctx, serviceName)
	if err != nil {
		return fmt.Errorf("creating tracer: %w", err)
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		tracerShutdown(shutdownCtx)
	}()

	// Metrics + health HTTP server.
	metricsPort := envutil.Get("METRICS_PORT", "9094")
	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	httpSrv := &http.Server{
		Addr:              ":" + metricsPort,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		logger.Info("http server starting", zap.String("port", metricsPort))
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server failed", zap.Error(err))
		}
	}()

	// PostgreSQL.
	pool, err := postgres.PoolFromEnv(ctx)
	if err != nil {
		return fmt.Errorf("connecting to postgres: %w", err)
	}
	defer pool.Close()

	// NATS.
	nc, err := sharednats.ClientFromEnv(logger, serviceName)
	if err != nil {
		return fmt.Errorf("connecting to nats: %w", err)
	}
	defer nc.Close()

	logger.Info("indexer worker started")

	// Wait for shutdown signal.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigCh
	logger.Info("received signal, shutting down", zap.String("signal", sig.String()))

	// Graceful shutdown.
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	httpSrv.Shutdown(shutdownCtx)

	logger.Info("shutdown complete")
	return nil
}
