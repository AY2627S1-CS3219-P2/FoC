package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"foc/chat-service/internal/chat"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type SuspensionChecker func(ctx context.Context, userID uuid.UUID) (bool, error)
type TokenVerifier func(ctx context.Context, token string) (uuid.UUID, error)

type RealtimeEnvelope struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

type ReadOnlyPayload struct {
	ConversationID uuid.UUID `json:"conversation_id"`
	ErrandID       uuid.UUID `json:"errand_id"`
}

type ReadReceiptPayload struct {
	ConversationID uuid.UUID `json:"conversation_id"`
	ReaderID       uuid.UUID `json:"reader_id"`
	ReadAt         time.Time `json:"read_at"`
}

type Hub struct {
	mu          sync.RWMutex
	clients     map[uuid.UUID]map[*Client]bool
	register    chan *Client
	unregister  chan *Client
	redisClient redis.UniversalClient
	isSuspended SuspensionChecker
	verifyToken TokenVerifier
	log         *slog.Logger
}

func NewHub(redisClient redis.UniversalClient, isSuspended SuspensionChecker, verifyToken TokenVerifier, log *slog.Logger) *Hub {
	return &Hub{
		clients:     make(map[uuid.UUID]map[*Client]bool),
		register:    make(chan *Client),
		unregister:  make(chan *Client),
		redisClient: redisClient,
		isSuspended: isSuspended,
		verifyToken: verifyToken,
		log:         log,
	}
}

func (h *Hub) Run(ctx context.Context) {
	if h.redisClient != nil {
		go h.subscribeRedis(ctx)
	}

	for {
		select {
		case <-ctx.Done():
			h.mu.Lock()
			for _, userClients := range h.clients {
				for client := range userClients {
					close(client.send)
					if client.conn != nil {
						_ = client.conn.Close()
					}
				}
			}
			h.clients = make(map[uuid.UUID]map[*Client]bool)
			h.mu.Unlock()
			return

		case client := <-h.register:
			h.mu.Lock()
			userClients, ok := h.clients[client.userID]
			if !ok {
				userClients = make(map[*Client]bool)
				h.clients[client.userID] = userClients
			}
			userClients[client] = true
			h.mu.Unlock()

		case client := <-h.unregister:
			h.mu.Lock()
			if userClients, ok := h.clients[client.userID]; ok {
				if _, ok := userClients[client]; ok {
					delete(userClients, client)
					close(client.send)
					if len(userClients) == 0 {
						delete(h.clients, client.userID)
					}
				}
			}
			h.mu.Unlock()
		}
	}
}

func (h *Hub) RegisterClient(client *Client) {
	h.register <- client
}

func (h *Hub) UnregisterClient(client *Client) {
	h.unregister <- client
}

func (h *Hub) BroadcastMessage(ctx context.Context, msg *chat.Message, recipientID uuid.UUID) error {
	env := RealtimeEnvelope{
		Type: "message.created",
		Data: msg,
	}
	return h.publishUserEvent(ctx, recipientID, env)
}

func (h *Hub) BroadcastReadOnly(ctx context.Context, conversationID, errandID, requesterID, courierID uuid.UUID) error {
	env := RealtimeEnvelope{
		Type: "conversation.read_only",
		Data: ReadOnlyPayload{
			ConversationID: conversationID,
			ErrandID:       errandID,
		},
	}
	_ = h.publishUserEvent(ctx, requesterID, env)
	_ = h.publishUserEvent(ctx, courierID, env)
	return nil
}

func (h *Hub) BroadcastReadReceipt(ctx context.Context, conversationID, readerID, recipientID uuid.UUID, readAt time.Time) error {
	env := RealtimeEnvelope{
		Type: "conversation.read",
		Data: ReadReceiptPayload{
			ConversationID: conversationID,
			ReaderID:       readerID,
			ReadAt:         readAt,
		},
	}
	return h.publishUserEvent(ctx, recipientID, env)
}

func (h *Hub) publishUserEvent(ctx context.Context, userID uuid.UUID, env RealtimeEnvelope) error {
	bytes, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal realtime event: %w", err)
	}

	if h.redisClient != nil {
		channel := fmt.Sprintf("chat:events:user:%s", userID.String())
		return h.redisClient.Publish(ctx, channel, bytes).Err()
	}

	// Local fallback if redisClient is nil (e.g. testing)
	h.dispatchLocal(userID, bytes)
	return nil
}

func (h *Hub) dispatchLocal(userID uuid.UUID, data []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	userClients, ok := h.clients[userID]
	if !ok {
		return
	}
	for client := range userClients {
		select {
		case client.send <- data:
		default:
			// Buffer full, drop or let writePump handle
		}
	}
}

func (h *Hub) subscribeRedis(ctx context.Context) {
	pubsub := h.redisClient.PSubscribe(ctx, "chat:events:user:*")
	defer func() { _ = pubsub.Close() }()

	ch := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			parts := strings.Split(msg.Channel, ":")
			if len(parts) != 4 {
				continue
			}
			targetID, err := uuid.Parse(parts[3])
			if err != nil {
				continue
			}
			h.dispatchLocal(targetID, []byte(msg.Payload))
		}
	}
}
