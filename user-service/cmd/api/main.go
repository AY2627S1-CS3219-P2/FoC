// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-18
// Scope: Added the user-service command entry point and wired the recorded startup dependencies.
// Author review: COMPLETED BY ZI YANG - verified correctness

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"time"

	"foc/user-service/internal/config"
	"foc/user-service/internal/httpapi/handlers"
	"foc/user-service/internal/httpapi/router"
	"foc/user-service/internal/httpapi/routes"
	"foc/user-service/internal/jwt"
	"foc/user-service/internal/relay"
	"foc/user-service/internal/repository"
	"foc/user-service/internal/session"
	"foc/user-service/internal/user"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// AI-generated (edited by James Teo).
const (
	// eventsExchange is the topic exchange RabbitMQ's definitions declare;
	// the relay publishes every outbox event to it and never declares it.
	eventsExchange = "foc.events"
	// defaultRabbitMQURL is the broker a host-run service uses when
	// RABBITMQ_URL is unset: Compose's RabbitMQ published on localhost.
	defaultRabbitMQURL = "amqp://foc:foc-dev-password@localhost:5672/foc"
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
	// go-redis ignores a context's deadline on socket reads and writes unless
	// this is set, so the revocation check's 500 ms bound would not hold against
	// a Redis that accepts the connection and then stops answering.
	redisOptions.ContextTimeoutEnabled = true
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
	// AI-generated (edited by James Teo): OTP_DELIVERY picks Mailgun or, for
	// local runs, the service log.
	otpDeliverer, err := newOTPDeliverer(cfg, logger)
	if err != nil {
		return fmt.Errorf("configure OTP delivery: %w", err)
	}
	// AI Assistance Disclosure: Codex (GPT-5), 2026-10-10 — wires the
	// owner-recorded OTP Redis and Mailgun boundaries into registration.
	// Author review: ZI YANG - validated correctness.
	accountService := user.NewAccountServiceWithOTPAndDelivery(
		userRepository,
		repository.NewRedisBlocklistWriter(redisClient),
		sessionRepository,
		accessTokenTTL+jwt.ClockSkew,
		repository.NewRedisOTPStore(redisClient),
		otpDeliverer, // AI-generated (edited by James Teo).
	)
	if err := bootstrapInitialAdmin(ctx, accountService, cfg.InitialAdminEmail, cfg.InitialAdminUsername, cfg.InitialAdminPassword); err != nil {
		return fmt.Errorf("bootstrap initial admin: %w", err)
	}
	// AI-generated (edited by James Teo): the background outbox worker. The
	// publisher dials RabbitMQ on its first publish, so the broker is not
	// needed to start; events wait in outbox_events until it is reachable.
	rabbitMQURL, err := parseRabbitMQURL(cfg.RabbitMQURL)
	if err != nil {
		return fmt.Errorf("parse RabbitMQ URL: %w", err)
	}
	publisher := relay.NewAMQPPublisher(rabbitMQURL, eventsExchange, logger)
	defer func() {
		if err := publisher.Close(); err != nil {
			logger.Warn("close RabbitMQ connection", "error", err)
		}
	}()
	stopRelay := startBackground(ctx, relay.New(relay.NewPostgresStore(pool), publisher, logger, relay.Options{}).Run)
	defer stopRelay()
	authenticator := session.NewAuthenticator(userRepository, jwtService)
	loginService := session.NewLoginService(authenticator, sessionRepository, time.Now)
	// AI-generated (edited by ZI YANG): supply the recorded Redis refresh lock through the composition root.
	refreshService := session.NewRefreshService(userRepository, sessionRepository, jwtService, jwtService, repository.NewRedisRefreshLock(redisClient), time.Now)
	logoutService := session.NewLogoutService(sessionRepository, repository.NewRedisBlocklistWriter(redisClient), time.Now)
	revocationReader := repository.NewRedisBlocklistReader(redisClient)
	httpHandler := router.Setup(routes.Dependencies{
		Auth: handlers.AuthDependencies{
			Registrar:      accountService,
			OTPResender:    accountService,
			OTPVerifier:    accountService,
			LoginService:   loginService,
			Refresher:      refreshService,
			AccessVerifier: jwtService,
			Logoutter:      logoutService,
			Logger:         logger,
		},
		Profile: handlers.ProfileDependencies{
			// AI-generated (edited by ZI YANG): profile reads enter through the domain service, not the repository adapter.
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
		TokenVerifier:     jwtPrincipalVerifier{service: jwtService},
		RevocationChecker: jwtRevocationChecker{reader: revocationReader},
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

type jwtRevocationChecker struct {
	reader *repository.RedisBlocklistReader
}

func (c jwtRevocationChecker) Check(ctx context.Context, principal handlers.Principal) (bool, error) {
	if c.reader == nil {
		return false, errors.New("Redis revocation reader is required")
	}
	return c.reader.IsRevoked(ctx, principal.JTI, principal.UserID, principal.IssuedAt)
}

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
	return handlers.Principal{UserID: verified.Subject, Role: verified.Role, JTI: verified.JTI, IssuedAt: verified.IssuedAt}, nil
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

// AI-generated (edited by James Teo).
// parseRabbitMQURL returns value, or defaultRabbitMQURL when it is empty,
// once it is known to be an amqp:// or amqps:// URL. The URL carries the
// broker password, so the error is fixed text that never quotes or wraps the
// value: net/url's error quotes the whole URL. Rejecting what net/url cannot
// parse here also means the AMQP client's dial error, which would quote it,
// never arises.
func parseRabbitMQURL(value string) (string, error) {
	if value == "" {
		return defaultRabbitMQURL, nil
	}
	if u, err := url.Parse(value); err != nil || (u.Scheme != "amqp" && u.Scheme != "amqps") {
		return "", errors.New("RABBITMQ_URL must be an amqp:// or amqps:// URL with reserved characters in the user and password percent-encoded")
	}
	return value, nil
}

// AI-generated (edited by James Teo).
// startBackground runs work in its own goroutine until the returned stop is
// called. stop cancels work's context and waits for work to return, so the
// relay's claim in flight ends before the database pool it uses is closed.
func startBackground(ctx context.Context, work func(context.Context)) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		work(ctx)
	}()
	return func() {
		cancel()
		<-done
	}
}
