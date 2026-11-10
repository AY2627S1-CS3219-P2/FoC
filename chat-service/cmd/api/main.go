package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"foc/chat-service/internal/auth"
	"foc/chat-service/internal/chat"
	"foc/chat-service/internal/config"
	"foc/chat-service/internal/consumer"
	"foc/chat-service/internal/httpapi"
	"foc/chat-service/internal/hub"
	"foc/chat-service/internal/media"
	"foc/chat-service/internal/repository"
	"foc/chat-service/internal/sweeper"
	"foc/chat-service/internal/ticket"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	// fatal logs why chat-service cannot run and exits, as log.Fatalf did.
	fatal := func(msg string, err error) {
		log.Error(msg, "err", err)
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		fatal("failed to load configuration", err)
	}

	log.Info("running database migrations", "dir", cfg.MigrationsDir)
	if err := repository.Migrate(cfg.ChatDBURL, cfg.MigrationsDir); err != nil {
		fatal("migration failed", err)
	}
	log.Info("database migrations applied successfully")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Connect PostgreSQL
	dbPool, err := repository.ConnectPool(ctx, cfg.ChatDBURL)
	if err != nil {
		fatal("failed to connect database pool", err)
	}
	defer dbPool.Close()

	// Connect Chat Redis (dedicated for handshake tickets and WebSocket pub/sub)
	chatRedisOpt, err := redis.ParseURL(cfg.ChatRedisURL)
	if err != nil {
		fatal("failed to parse CHAT_REDIS_URL", redactURLError(err))
	}
	chatRedisClient := redis.NewClient(chatRedisOpt)
	defer func() { _ = chatRedisClient.Close() }()

	// Connect Auth Redis (shared, read-only for session revocation and suspension)
	authRedisOpt, err := redis.ParseURL(cfg.AuthRedisURL)
	if err != nil {
		fatal("failed to parse AUTH_REDIS_URL", redactURLError(err))
	}
	authRedisClient := redis.NewClient(authRedisOpt)
	defer func() { _ = authRedisClient.Close() }()

	// Initialize adapters
	repo := repository.NewPostgresRepository(dbPool)
	ticketStore := ticket.NewStore(chatRedisClient)
	mediaStore, err := media.NewStorage(cfg.MediaStoragePath)
	if err != nil {
		fatal("failed to initialize media storage", err)
	}

	// Zero-trust verifier
	verifier := auth.NewVerifier(cfg.JWKSURL, authRedisClient)

	// WebSocket Hub
	tokenRefresher := func(ctx context.Context, token string) (uuid.UUID, error) {
		claims, err := verifier.Verify(ctx, token)
		if err != nil {
			return uuid.Nil, err
		}
		return claims.UserID, nil
	}
	wsHub := hub.NewHub(chatRedisClient, verifier.IsUserSuspended, tokenRefresher, log)
	go wsHub.Run(ctx)

	// Domain Service
	chatService := chat.NewService(repo, wsHub, nil)

	// Background Sweeper
	expSweeper := sweeper.NewSweeper(chatService, 60*time.Second, log)
	expSweeper.Start(ctx)
	defer expSweeper.Stop()

	// RabbitMQ Lifecycle Event Consumer
	lifecycleHandler := consumer.NewLifecycleHandler(chatService, log)
	go func() {
		for {
			if ctx.Err() != nil {
				return
			}
			log.Info("connecting RabbitMQ consumer", "queue", consumer.ChatQueue)
			if err := consumer.RunConsumer(ctx, cfg.RabbitMQURL, lifecycleHandler); err != nil && ctx.Err() == nil {
				log.Warn("rabbitmq consumer error, retrying in 5 seconds", "queue", consumer.ChatQueue, "err", redactURLError(err))
				time.Sleep(5 * time.Second)
			}
		}
	}()

	// HTTP API Handler & Router
	handler := httpapi.NewHandler(chatService, ticketStore, mediaStore, log)
	router := httpapi.NewRouter(handler, wsHub, verifier, log)

	server := newHTTPServer(":"+cfg.Port, router, log)

	// Graceful shutdown listener
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Info("chat-service listening", "port", cfg.Port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fatal("http server error", err)
		}
	}()

	<-shutdown
	log.Info("shutdown signal received, draining connections")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error("server shutdown error", "err", err)
	}
	log.Info("chat-service shut down cleanly")
}

// newHTTPServer serves handler on addr, with net/http's own errors written to
// log as JSON at ERROR rather than as text through the standard log package.
func newHTTPServer(addr string, handler http.Handler, log *slog.Logger) *http.Server {
	return &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		ErrorLog:     slog.NewLogLogger(log.Handler(), slog.LevelError),
	}
}

// redactURLError keeps a URL parse error's text out of the log: it quotes the
// whole URL, password included.
func redactURLError(err error) error {
	if errors.As(err, new(*url.Error)) {
		return errors.New("not a valid URL")
	}
	return err
}
