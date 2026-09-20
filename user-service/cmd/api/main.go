// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-18
// Scope: Added the user-service command entry point and wired the recorded startup dependencies.
// Author review: COMPLETED BY ZI YANG

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"foc/user-service/internal/auth"
	"foc/user-service/internal/config"
	"foc/user-service/internal/httpapi"
	"foc/user-service/internal/repository"
	"foc/user-service/internal/user"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func main() {
	cfg := config.Load()
	ctx := context.Background()

	if err := run(ctx, cfg); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, cfg config.Config) error {
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

	keySet, err := auth.LoadKeySet(cfg.JWTKeySetPath)
	if err != nil {
		return fmt.Errorf("load JWT key set: %w", err)
	}
	accessTokenTTL, err := parseTokenTTL(cfg.JWTAccessTokenTTL, auth.DefaultAccessTokenTTL)
	if err != nil {
		return fmt.Errorf("parse JWT access-token TTL: %w", err)
	}
	refreshTokenTTL, err := parseTokenTTL(cfg.JWTRefreshTokenTTL, auth.DefaultRefreshTokenTTL)
	if err != nil {
		return fmt.Errorf("parse JWT refresh-token TTL: %w", err)
	}
	jwtService, err := auth.NewServiceWithTokenTTLs(keySet, time.Now, accessTokenTTL, refreshTokenTTL)
	if err != nil {
		return fmt.Errorf("create JWT service: %w", err)
	}

	userRepository := repository.NewPostgresRepository(pool)
	sessionRepository := repository.NewPostgresSessionRepository(pool)
	accountService := user.NewAccountService(userRepository)
	authenticator := user.NewAuthenticator(userRepository, jwtService)
	loginService := user.NewLoginService(authenticator, sessionRepository, time.Now)
	refreshService := user.NewRefreshService(userRepository, sessionRepository, jwtService, jwtService, time.Now)
	logoutService := user.NewLogoutService(sessionRepository, repository.NewRedisBlocklistWriter(redisClient), time.Now)
	router := httpapi.NewRouter(httpapi.Dependencies{
		Registrar:      accountService,
		ProfileGetter:  userRepository,
		StatusUpdater:  accountService,
		ProfileUpdater: accountService,
		TokenVerifier:  jwtPrincipalVerifier{service: jwtService},
		LoginService:   loginService,
		Refresher:      refreshService,
		AccessVerifier: jwtService,
		Logoutter:      logoutService,
		JWKSProvider:   jwtService,
		HealthCheck:    pool.Ping,
	})

	server := &http.Server{Addr: ":" + cfg.Port, Handler: router}
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve HTTP: %w", err)
	}
	return nil
}

type jwtPrincipalVerifier struct{ service *auth.Service }

func (v jwtPrincipalVerifier) Verify(ctx context.Context, rawToken string) (httpapi.Principal, error) {
	if err := ctx.Err(); err != nil {
		return httpapi.Principal{}, err
	}
	if v.service == nil {
		return httpapi.Principal{}, errors.New("JWT service is required")
	}
	verified, err := v.service.Verify(rawToken)
	if err != nil {
		return httpapi.Principal{}, err
	}
	if verified.Type != auth.AccessToken {
		return httpapi.Principal{}, errors.New("JWT is not an access token")
	}
	return httpapi.Principal{UserID: verified.Subject, Role: verified.Role}, nil
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
