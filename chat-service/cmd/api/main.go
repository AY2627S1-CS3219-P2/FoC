package main

import (
	"context"
	"errors"
	"log"
	"net/http"
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
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load configuration: %v", err)
	}

	log.Printf("running database migrations from %s...", cfg.MigrationsDir)
	if err := repository.Migrate(cfg.ChatDBURL, cfg.MigrationsDir); err != nil {
		log.Fatalf("migration failed: %v", err)
	}
	log.Printf("database migrations applied successfully")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Connect PostgreSQL
	dbPool, err := repository.ConnectPool(ctx, cfg.ChatDBURL)
	if err != nil {
		log.Fatalf("failed to connect database pool: %v", err)
	}
	defer dbPool.Close()

	// Connect Chat Redis (dedicated for handshake tickets and WebSocket pub/sub)
	chatRedisOpt, err := redis.ParseURL(cfg.ChatRedisURL)
	if err != nil {
		log.Fatalf("failed to parse CHAT_REDIS_URL: %v", err)
	}
	chatRedisClient := redis.NewClient(chatRedisOpt)
	defer func() { _ = chatRedisClient.Close() }()

	// Connect Auth Redis (shared, read-only for session revocation and suspension)
	authRedisOpt, err := redis.ParseURL(cfg.AuthRedisURL)
	if err != nil {
		log.Fatalf("failed to parse AUTH_REDIS_URL: %v", err)
	}
	authRedisClient := redis.NewClient(authRedisOpt)
	defer func() { _ = authRedisClient.Close() }()

	// Initialize adapters
	repo := repository.NewPostgresRepository(dbPool)
	ticketStore := ticket.NewStore(chatRedisClient)
	mediaStore, err := media.NewStorage(cfg.MediaStoragePath)
	if err != nil {
		log.Fatalf("failed to initialize media storage: %v", err)
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
	wsHub := hub.NewHub(chatRedisClient, verifier.IsUserSuspended, tokenRefresher)
	go wsHub.Run(ctx)

	// Domain Service
	chatService := chat.NewService(repo, wsHub, nil)

	// Background Sweeper
	expSweeper := sweeper.NewSweeper(chatService, 60*time.Second)
	expSweeper.Start(ctx)
	defer expSweeper.Stop()

	// RabbitMQ Lifecycle Event Consumer
	lifecycleHandler := consumer.NewLifecycleHandler(chatService)
	go func() {
		for {
			if ctx.Err() != nil {
				return
			}
			log.Printf("connecting RabbitMQ consumer on %s...", consumer.ChatQueue)
			if err := consumer.RunConsumer(ctx, cfg.RabbitMQURL, lifecycleHandler); err != nil && ctx.Err() == nil {
				log.Printf("rabbitmq consumer error: %v, retrying in 5 seconds...", err)
				time.Sleep(5 * time.Second)
			}
		}
	}()

	// HTTP API Handler & Router
	handler := httpapi.NewHandler(chatService, ticketStore, mediaStore)
	router := httpapi.NewRouter(handler, wsHub, verifier)

	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	// Graceful shutdown listener
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("chat-service listening on port %s", cfg.Port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http server error: %v", err)
		}
	}()

	<-shutdown
	log.Printf("shutdown signal received, draining connections...")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("server shutdown error: %v", err)
	}
	log.Printf("chat-service shut down cleanly")
}
