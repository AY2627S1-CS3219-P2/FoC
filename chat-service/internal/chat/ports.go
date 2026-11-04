package chat

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Clock interface {
	Now() time.Time
}

type RealClock struct{}

func (RealClock) Now() time.Time {
	return time.Now().UTC()
}

type Repository interface {
	GetConversation(ctx context.Context, id uuid.UUID) (*Conversation, error)
	GetConversationByErrandCourier(ctx context.Context, errandID, courierID uuid.UUID) (*Conversation, error)
	ListConversationsByUser(ctx context.Context, userID uuid.UUID, errandIDFilter *uuid.UUID) ([]*Conversation, error)
	ListAllConversationsAdmin(ctx context.Context, errandIDFilter *uuid.UUID, limit int) ([]*Conversation, error)

	UpsertConversationOnAccepted(ctx context.Context, errandID, requesterID, courierID uuid.UUID, occurredAt time.Time) (*Conversation, error)
	FreezeConversation(ctx context.Context, errandID, courierID uuid.UUID, occurredAt time.Time) error
	MarkErrandCompleted(ctx context.Context, errandID uuid.UUID, occurredAt time.Time) error
	FreezeErrandCancelledOrExpired(ctx context.Context, errandID uuid.UUID, occurredAt time.Time) error
	FreezeConversationsPast24Hours(ctx context.Context, cutoff time.Time) (int64, error)

	// SaveMessage idempotently saves a message by (conversation_id, client_message_id).
	// If duplicate, returns (existingMessage, true, nil).
	SaveMessage(ctx context.Context, msg *Message) (*Message, bool, error)
	GetMessage(ctx context.Context, messageID uuid.UUID) (*Message, error)
	ListMessages(ctx context.Context, conversationID uuid.UUID, cursor *MessageCursor, since *time.Time, limit int) ([]*Message, *MessageCursor, bool, error)

	UpdateLastRead(ctx context.Context, conversationID, userID uuid.UUID, readAt time.Time) error
	GetUnreadSummary(ctx context.Context, userID uuid.UUID) (hasUnread bool, unreadErrandIDs []uuid.UUID, err error)
	RecordAudit(ctx context.Context, entry *AuditEntry) error
}

type EventBroadcaster interface {
	BroadcastMessage(ctx context.Context, msg *Message, recipientID uuid.UUID) error
	BroadcastReadOnly(ctx context.Context, conversationID, errandID, requesterID, courierID uuid.UUID) error
	BroadcastReadReceipt(ctx context.Context, conversationID, readerID, recipientID uuid.UUID, readAt time.Time) error
}
