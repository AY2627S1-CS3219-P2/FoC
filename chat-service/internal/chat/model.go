package chat

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	StatusActive = "ACTIVE"
	StatusFrozen = "FROZEN"

	MessageTypeText  = "TEXT"
	MessageTypeImage = "IMAGE"

	RoleRequester = "REQUESTER"
	RoleCourier   = "COURIER"
	RoleAdmin     = "ADMIN"

	AuditActionInspect = "chat.inspect"
)

type Conversation struct {
	ID                  uuid.UUID  `json:"id"`
	ErrandID            uuid.UUID  `json:"errand_id"`
	RequesterID         uuid.UUID  `json:"requester_id"`
	CourierID           uuid.UUID  `json:"courier_id"`
	Status              string     `json:"status"`
	ErrandCompletedAt   *time.Time `json:"errand_completed_at,omitempty"`
	RequesterLastReadAt time.Time  `json:"requester_last_read_at"`
	CourierLastReadAt   time.Time  `json:"courier_last_read_at"`
	LastEventOccurredAt time.Time  `json:"last_event_occurred_at"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

// IsReadOnly determines if messages can no longer be sent.
// A conversation is read-only if explicitly FROZEN or if 24 hours have elapsed since errand completion.
func (c *Conversation) IsReadOnly(now time.Time) bool {
	if c.Status == StatusFrozen {
		return true
	}
	if c.ErrandCompletedAt != nil {
		cutoff := c.ErrandCompletedAt.Add(24 * time.Hour)
		return !now.Before(cutoff)
	}
	return false
}

// ParticipantRole returns whether userID is the requester or courier.
func (c *Conversation) ParticipantRole(userID uuid.UUID) (string, bool) {
	if c.RequesterID == userID {
		return RoleRequester, true
	}
	if c.CourierID == userID {
		return RoleCourier, true
	}
	return "", false
}

// LastReadAt returns the caller's read pointer.
func (c *Conversation) LastReadAt(userID uuid.UUID) time.Time {
	if c.RequesterID == userID {
		return c.RequesterLastReadAt
	}
	if c.CourierID == userID {
		return c.CourierLastReadAt
	}
	return time.Time{}
}

// OtherParticipantID returns the opposite party ID.
func (c *Conversation) OtherParticipantID(userID uuid.UUID) (uuid.UUID, bool) {
	if c.RequesterID == userID {
		return c.CourierID, true
	}
	if c.CourierID == userID {
		return c.RequesterID, true
	}
	return uuid.Nil, false
}

type Message struct {
	ID              uuid.UUID  `json:"id"`
	ConversationID  uuid.UUID  `json:"conversation_id"`
	SenderID        uuid.UUID  `json:"sender_id"`
	Type            string     `json:"message_type"`
	TextContent     *string    `json:"text_content,omitempty"`
	MediaID         *uuid.UUID `json:"media_id,omitempty"`
	MediaURL        *string    `json:"media_url,omitempty"`
	ClientMessageID *uuid.UUID `json:"client_message_id,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

type MessageCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

func (c MessageCursor) String() string {
	return fmt.Sprintf("%s_%s", c.CreatedAt.Format(time.RFC3339Nano), c.ID.String())
}

func ParseCursor(raw string) (*MessageCursor, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	parts := strings.SplitN(raw, "_", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid cursor format")
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid cursor timestamp: %w", err)
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid cursor ID: %w", err)
	}
	return &MessageCursor{CreatedAt: t, ID: id}, nil
}

type MessagePage struct {
	Messages   []*Message     `json:"messages"`
	NextCursor *MessageCursor `json:"next_cursor,omitempty"`
	HasMore    bool           `json:"has_more"`
}

type AuditEntry struct {
	ID             uuid.UUID `json:"id"`
	AdminID        uuid.UUID `json:"admin_id"`
	Action         string    `json:"action"`
	ConversationID uuid.UUID `json:"conversation_id"`
	ErrandID       uuid.UUID `json:"errand_id"`
	OccurredAt     time.Time `json:"occurredAt"`
}

type Caller struct {
	UserID uuid.UUID
	Role   string
}
