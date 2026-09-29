// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-18
// Scope: Added the user-service command entry point and wired the recorded startup dependencies.
// Author review: COMPLETED BY ZI YANG - verified correctness

// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-29
// Scope: Switched process wiring from chi HTTP to gRPC plus the JWKS HTTP exception.
// Author review: ZI YANG - Validated correctness

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"foc/user-service/internal/config"
	"foc/user-service/internal/grpcapi"
	"foc/user-service/internal/jwkshttp"
	"foc/user-service/internal/jwt"
	"foc/user-service/internal/repository"
	"foc/user-service/internal/session"
	"foc/user-service/internal/user"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, cfg, logger); err != nil {
		logger.Error("user service stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	if err := repository.Apply(cfg.UserDBURL, "file://migrations"); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	pool, err := pgxpool.New(ctx, cfg.UserDBURL)
	if err != nil {
		return fmt.Errorf("create database pool: %w", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	redisOptions, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return fmt.Errorf("parse Redis URL: %w", err)
	}
	redisClient := redis.NewClient(redisOptions)
	defer func() { _ = redisClient.Close() }()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping Redis: %w", err)
	}

	keySet, err := jwt.LoadKeySet(cfg.JWTKeySetPath)
	if err != nil {
		return fmt.Errorf("load JWT key set: %w", err)
	}
	accessTokenTTL, err := parseTokenTTL(cfg.JWTAccessTokenTTL, jwt.DefaultAccessTokenTTL)
	if err != nil {
		return fmt.Errorf("parse JWT access-token TTL: %w", err)
	}
	refreshTokenTTL, err := parseTokenTTL(cfg.JWTRefreshTokenTTL, jwt.DefaultRefreshTokenTTL)
	if err != nil {
		return fmt.Errorf("parse JWT refresh-token TTL: %w", err)
	}
	jwtService, err := jwt.NewServiceWithTokenTTLs(keySet, time.Now, accessTokenTTL, refreshTokenTTL)
	if err != nil {
		return fmt.Errorf("create JWT service: %w", err)
	}

	userRepository := repository.NewPostgresRepository(pool)
	sessionRepository := repository.NewPostgresSessionRepository(pool)
	accountService := user.NewAccountServiceWithClock(
		userRepository,
		repository.NewRedisBlocklistWriter(redisClient),
		sessionRepository,
		accessTokenTTL,
		time.Now,
	)
	if err := bootstrapInitialAdmin(ctx, accountService, cfg.InitialAdminEmail, cfg.InitialAdminUsername, cfg.InitialAdminPassword); err != nil {
		return fmt.Errorf("bootstrap initial admin: %w", err)
	}
	authenticator := session.NewAuthenticator(userRepository, jwtService)
	loginService := session.NewLoginService(authenticator, sessionRepository, time.Now)
	// AI-generated (edited by ZI YANG): supply the recorded Redis refresh lock through the composition root.
	refreshService := session.NewRefreshService(userRepository, sessionRepository, jwtService, jwtService, repository.NewRedisRefreshLock(redisClient), time.Now)
	logoutService := session.NewLogoutService(sessionRepository, repository.NewRedisBlocklistWriter(redisClient), time.Now)
	userServer := grpcapi.NewServer(grpcapi.Dependencies{
		Registrar:           accountService,
		LoginService:        loginService,
		ProfileGetter:       accountService,
		ProfileUpdater:      accountService,
		AdminProfileUpdater: accountService,
		StatusUpdater:       accountService,
		Refresher:           refreshService,
		AccessVerifier:      jwtService,
		Logoutter:           logoutService,
	})
	healthServer, err := grpcapi.NewHealthServer(
		pool.Ping,
		func(ctx context.Context) error { return redisClient.Ping(ctx).Err() },
	)
	if err != nil {
		return fmt.Errorf("create health service: %w", err)
	}
	grpcServer, err := grpcapi.NewGRPCServer(logger, userServer, healthServer)
	if err != nil {
		return fmt.Errorf("create gRPC server: %w", err)
	}
	jwksHandler, err := jwkshttp.NewHandler(jwtService, logger)
	if err != nil {
		return fmt.Errorf("create JWKS handler: %w", err)
	}
	jwksServer := newJWKSHTTPServer(jwksHandler)
	grpcListener, jwksListener, err := openServiceListeners(net.Listen, cfg.Port)
	if err != nil {
		return err
	}
	return serveServers(ctx, grpcServer, grpcListener, jwksServer, jwksListener)
}

// newJWKSHTTPServer constructs the isolated key-discovery server with the
// recorded defensive timeouts.
func newJWKSHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              jwksListenAddress,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}

func parseTokenTTL(value string, defaultValue time.Duration) (time.Duration, error) {
	if value == "" {
		return defaultValue, nil
	}
	ttl, err := time.ParseDuration(value)
	if err != nil {
		return 0, err
	}
	if ttl <= 0 {
		return 0, errors.New("TTL must be positive")
	}
	return ttl, nil
}
