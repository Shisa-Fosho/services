package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc/health"

	"github.com/ethereum/go-ethereum/common"

	"github.com/Shisa-Fosho/services/internal/data"
	"github.com/Shisa-Fosho/services/internal/session"
	"github.com/Shisa-Fosho/services/internal/shared/auth"
	"github.com/Shisa-Fosho/services/internal/shared/eth"
	sharedgrpc "github.com/Shisa-Fosho/services/internal/shared/grpc"
	"github.com/Shisa-Fosho/services/internal/shared/httputil"
	sharednats "github.com/Shisa-Fosho/services/internal/shared/nats"
	"github.com/Shisa-Fosho/services/internal/shared/observability"
	"github.com/Shisa-Fosho/services/internal/shared/postgres"
	platformv1 "github.com/Shisa-Fosho/services/proto/gen/platform/v1"
)

const serviceName = "platform"

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

	// Metrics HTTP server.
	metricsPort := getEnv("METRICS_PORT", "9092")
	metricsSrv := &http.Server{
		Addr:              ":" + metricsPort,
		Handler:           metrics.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		logger.Info("metrics server starting", zap.String("port", metricsPort))
		if err := metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("metrics server failed", zap.Error(err))
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

	// Auth dependencies.
	jwtCfg := auth.JWTConfig{
		AccessSecret:  []byte(mustGetEnv("JWT_ACCESS_SECRET")),
		RefreshSecret: []byte(mustGetEnv("JWT_REFRESH_SECRET")),
		AccessTTL:     15 * time.Minute,
		RefreshTTL:    7 * 24 * time.Hour,
		Issuer:        "shisa-platform",
	}
	jwtMgr, err := auth.NewJWTManager(jwtCfg)
	if err != nil {
		return fmt.Errorf("creating jwt manager: %w", err)
	}

	siweDomain := getEnv("SIWE_DOMAIN", "localhost")
	siweVerifier := auth.NewSIWEVerifier(auth.SIWEConfig{
		Domain: siweDomain,
	})

	safeCfg := eth.SafeConfig{
		FactoryAddress:   common.HexToAddress(getEnv("SAFE_FACTORY_ADDRESS", "0xa6B71E26C5e0845f74c812102Ca7114b6a896AB2")),
		SingletonAddress: common.HexToAddress(getEnv("SAFE_SINGLETON_ADDRESS", "0xd9Db270c1B5E3Bd161E8c8503c55cEABeE709552")),
		FallbackHandler:  common.HexToAddress(getEnv("SAFE_FALLBACK_HANDLER", "0xf48f2B2d2a534e402487b3ee7C18c33Aec0Fe5e4")),
	}

	// API-key issuance has moved to the trading service; platform no longer
	// loads APIKEY_* env vars. See cmd/trading/main.go.

	repo := data.NewPGRepository(pool)
	secureCookies := siweDomain != "localhost"
	sessionHandler := session.NewHandler(logger, repo, jwtMgr, siweVerifier, safeCfg, secureCookies)

	// gRPC server.
	hs := health.NewServer()
	checker := sharedgrpc.NewPoolHealthChecker(pool)
	go sharedgrpc.WatchHealth(ctx, hs, serviceName, checker, 10*time.Second, logger)

	grpcSrv := sharedgrpc.NewServer(logger, metrics, hs)
	platformv1.RegisterPlatformServiceServer(grpcSrv, &platformServer{})

	// HTTP API server.
	httpPort := getEnv("HTTP_PORT", "8081")
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	sessionHandler.RegisterRoutes(mux)

	// Middleware stack (outermost first): Recovery → RequestID → Logging.
	var handler http.Handler = mux
	handler = httputil.Logging(logger)(handler)
	handler = httputil.RequestID(handler)
	handler = httputil.Recovery(logger)(handler)

	httpSrv := &http.Server{
		Addr:              ":" + httpPort,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		logger.Info("http server starting", zap.String("port", httpPort))
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server failed", zap.Error(err))
		}
	}()

	// Start gRPC listener.
	grpcPort := getEnv("GRPC_PORT", "9002")
	lis, err := net.Listen("tcp", ":"+grpcPort)
	if err != nil {
		return fmt.Errorf("listening on grpc port %s: %w", grpcPort, err)
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("grpc server starting", zap.String("port", grpcPort))
		errCh <- grpcSrv.Serve(lis)
	}()

	// Wait for shutdown signal or error.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		logger.Info("received signal, shutting down", zap.String("signal", sig.String()))
	case err := <-errCh:
		return fmt.Errorf("grpc server error: %w", err)
	}

	// Graceful shutdown.
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	httpSrv.Shutdown(shutdownCtx)
	metricsSrv.Shutdown(shutdownCtx)
	grpcSrv.GracefulStop()

	logger.Info("shutdown complete")
	return nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func mustGetEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic(fmt.Sprintf("required environment variable %s is not set", key))
	}
	return v
}

// platformServer implements the placeholder PlatformService.
type platformServer struct {
	platformv1.UnimplementedPlatformServiceServer
}

func (s *platformServer) GetStatus(ctx context.Context, _ *platformv1.GetStatusRequest) (*platformv1.GetStatusResponse, error) {
	return &platformv1.GetStatusResponse{Status: "ok"}, nil
}
