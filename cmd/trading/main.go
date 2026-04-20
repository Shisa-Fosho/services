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

	"github.com/Shisa-Fosho/services/internal/shared/envutil"
	sharedgrpc "github.com/Shisa-Fosho/services/internal/shared/grpc"
	"github.com/Shisa-Fosho/services/internal/shared/httputil"
	sharednats "github.com/Shisa-Fosho/services/internal/shared/nats"
	"github.com/Shisa-Fosho/services/internal/shared/observability"
	"github.com/Shisa-Fosho/services/internal/shared/postgres"
	"github.com/Shisa-Fosho/services/internal/shared/ratelimit"
	tradingauth "github.com/Shisa-Fosho/services/internal/trading/auth"
	tradingv1 "github.com/Shisa-Fosho/services/proto/gen/trading/v1"
)

const serviceName = "trading"

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
	metricsPort := envutil.Get("METRICS_PORT", "9091")
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

	// API-key auth dependencies. Trading owns the Polymarket-compatible
	// API-key lifecycle (derive, list, revoke) — see internal/trading/auth.
	apiKeyCfg := tradingauth.APIKeyConfig{
		DerivationSecret: []byte(envutil.MustGet("APIKEY_DERIVATION_SECRET")),
		EncryptionKey:    []byte(envutil.MustGet("APIKEY_ENCRYPTION_KEY")),
		ChainID:          137, // Polygon mainnet. Override via config for testnet/local.
	}
	if err := tradingauth.ValidateAPIKeyConfig(apiKeyCfg); err != nil {
		return fmt.Errorf("validating api key config: %w", err)
	}
	apiKeyRepo := tradingauth.NewPGRepository(pool)

	// Rate limiter: shared across all endpoints, with a strict "auth" profile
	// wrapping credential-verify routes (L1 EIP-712 + L2 HMAC) and a "default"
	// IP-keyed backstop wrapping the whole service.
	trustProxy := ratelimit.TrustProxyFromEnv()
	limiter, err := ratelimit.NewLimiter(ratelimit.Config{
		Profiles:           ratelimit.LoadProfilesFromEnv(),
		UserExtractor:      tradingauth.UserAddressFrom,
		TrustProxyHeaders:  trustProxy,
		Metrics:            metrics,
		Logger:             logger,
		LockoutsMaxEntries: ratelimit.LockoutsMaxFromEnv(),
		SweepBatchSize:     ratelimit.SweepBatchSizeFromEnv(),
	})
	if err != nil {
		return fmt.Errorf("creating rate limiter: %w", err)
	}
	go limiter.Start(ctx, ratelimit.SweepIntervalFromEnv())

	authProfile, _ := limiter.Profile("auth")
	apiKeyHandler := tradingauth.NewHandler(logger, apiKeyRepo, apiKeyCfg,
		tradingauth.WithHandlerAuthFailureHook(func(r *http.Request) {
			limiter.RecordAuthFailure(ratelimit.ClientIP(r, trustProxy), authProfile)
		}),
	)

	// gRPC server.
	hs := health.NewServer()
	checker := sharedgrpc.NewPoolHealthChecker(pool)
	go sharedgrpc.WatchHealth(ctx, hs, serviceName, checker, 10*time.Second, logger)

	grpcSrv := sharedgrpc.NewServer(logger, metrics, hs)
	tradingv1.RegisterTradingServiceServer(grpcSrv, &tradingServer{})

	// HTTP API server.
	httpPort := envutil.Get("HTTP_PORT", "8080")
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	apiKeyHandler.RegisterRoutes(mux, limiter.Middleware("auth", ratelimit.KeyByIP))

	// Middleware stack (outermost first):
	//   Recovery → RequestID → RateLimit(default,IP) → Logging → mux
	// The default limiter sits inside RequestID (so 429s carry X-Request-ID)
	// and outside Logging (so rejected requests are still logged).
	var handler http.Handler = mux
	handler = httputil.Logging(logger)(handler)
	handler = limiter.Middleware("default", ratelimit.KeyByIP)(handler)
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
	grpcPort := envutil.Get("GRPC_PORT", "9001")
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

// tradingServer implements the placeholder TradingService.
type tradingServer struct {
	tradingv1.UnimplementedTradingServiceServer
}

func (s *tradingServer) GetStatus(ctx context.Context, _ *tradingv1.GetStatusRequest) (*tradingv1.GetStatusResponse, error) {
	return &tradingv1.GetStatusResponse{Status: "ok"}, nil
}
