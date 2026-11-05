package ticket

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

var (
	ErrInvalidTicket = errors.New("invalid, expired, or already redeemed handshake ticket")
)

const (
	TicketTTL    = 30 * time.Second
	ticketPrefix = "chat:ticket:"
)

type Store struct {
	client redis.UniversalClient
}

func NewStore(client redis.UniversalClient) *Store {
	return &Store{client: client}
}

// IssueTicket generates an ephemeral 30-second single-use ticket for WebSocket authentication.
func (s *Store) IssueTicket(ctx context.Context, userID uuid.UUID) (string, error) {
	ticket := uuid.New().String()
	key := ticketPrefix + ticket

	err := s.client.Set(ctx, key, userID.String(), TicketTTL).Err()
	if err != nil {
		return "", fmt.Errorf("issue ticket: %w", err)
	}
	return ticket, nil
}

// RedeemTicket atomically retrieves and deletes the ticket, returning the associated userID.
func (s *Store) RedeemTicket(ctx context.Context, ticket string) (uuid.UUID, error) {
	if ticket == "" {
		return uuid.Nil, ErrInvalidTicket
	}
	key := ticketPrefix + ticket

	val, err := s.client.GetDel(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return uuid.Nil, ErrInvalidTicket
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("redeem ticket: %w", err)
	}

	userID, err := uuid.Parse(val)
	if err != nil {
		return uuid.Nil, ErrInvalidTicket
	}
	return userID, nil
}
