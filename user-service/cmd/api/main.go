// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-18
// Scope: Added the user-service command entry point and wired the recorded startup dependencies.
// Author review: COMPLETED BY ZI YANG

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"foc/user-service/internal/config"
	"foc/user-service/internal/httpapi/handlers"
	"foc/user-service/internal/httpapi/router"
	"foc/user-service/internal/httpapi/routes"
	"foc/user-service/internal/jwt"
	"foc/user-service/internal/repository"
	"foc/user-service/internal/session"
	"foc/user-service/internal/user"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func main() {
	cfg := config.Load()
	ctx := context.Background()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

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
	refreshService := session.NewRefreshService(userRepository, sessionRepository, jwtService, jwtService, time.Now)
	logoutService := session.NewLogoutService(sessionRepository, repository.NewRedisBlocklistWriter(redisClient), time.Now)
	httpHandler := router.Setup(routes.Dependencies{
		Auth: handlers.AuthDependencies{
			Registrar:      accountService,
			LoginService:   loginService,
			Refresher:      refreshService,
			AccessVerifier: jwtService,
			Logoutter:      logoutService,
			Logger:         logger,
		},
		Profile: handlers.ProfileDependencies{
			// AI-generated (edited by PENDING): profile reads enter through the domain service, not the repository adapter.
			ProfileGetter:       accountService,
			StatusUpdater:       accountService,
			ProfileUpdater:      accountService,
			AdminProfileUpdater: accountService,
			Logger:              logger,
		},
		System: handlers.SystemDependencies{
			JWKSProvider: jwtService,
			HealthCheck: newHealthCheck(
				pool.Ping,
				func(ctx context.Context) error { return redisClient.Ping(ctx).Err() },
			),
			Logger: logger,
		},
		TokenVerifier: jwtPrincipalVerifier{service: jwtService},
	})

	server := newHTTPServer(":"+cfg.Port, httpHandler)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve HTTP: %w", err)
	}
	return nil
}

// newHTTPServer constructs the API server with the recorded defensive timeouts.
func newHTTPServer(addr string, handler http.Handler) *http.Server {
	// AI-generated (edited by ZI YANG): apply the owner-recorded HTTP timeout policy at server construction.
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}

// newHealthCheck reports failure when any required service dependency is unavailable.
func newHealthCheck(checks ...func(context.Context) error) handlers.HealthCheck {
	// AI-generated (edited by ZI YANG): Redis and PostgreSQL are both required for the recorded health boundary.
	return func(ctx context.Context) error {
		for _, check := range checks {
			if err := check(ctx); err != nil {
				return err
			}
		}
		return nil
	}
}

type jwtPrincipalVerifier struct{ service *jwt.Service }

func (v jwtPrincipalVerifier) Verify(ctx context.Context, rawToken string) (handlers.Principal, error) {
	if err := ctx.Err(); err != nil {
		return handlers.Principal{}, err
	}
	if v.service == nil {
		return handlers.Principal{}, errors.New("JWT service is required")
	}
	verified, err := v.service.Verify(rawToken)
	if err != nil {
		return handlers.Principal{}, err
	}
	if verified.Type != jwt.AccessToken {
		return handlers.Principal{}, errors.New("JWT is not an access token")
	}
	return handlers.Principal{UserID: verified.Subject, Role: verified.Role}, nil
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
