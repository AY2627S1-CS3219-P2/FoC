package chat

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type inMemoryChatRepo struct {
	convs         map[uuid.UUID]*Conversation
	messages      []*Message
	lastReadTimes map[string]time.Time
	auditLogs     []*AuditEntry
}

func newInMemoryRepo() *inMemoryChatRepo {
	return &inMemoryChatRepo{
		convs:         make(map[uuid.UUID]*Conversation),
		lastReadTimes: make(map[string]time.Time),
	}
}

func (m *inMemoryChatRepo) GetConversation(ctx context.Context, id uuid.UUID) (*Conversation, error) {
	c, ok := m.convs[id]
	if !ok {
		return nil, ErrConversationNotFound
	}
	return c, nil
}

func (m *inMemoryChatRepo) GetConversationByErrandCourier(ctx context.Context, errandID, courierID uuid.UUID) (*Conversation, error) {
	for _, c := range m.convs {
		if c.ErrandID == errandID && c.CourierID == courierID {
			return c, nil
		}
	}
	return nil, ErrConversationNotFound
}

func (m *inMemoryChatRepo) ListConversationsByUser(ctx context.Context, userID uuid.UUID, errandIDFilter *uuid.UUID) ([]*Conversation, error) {
	var list []*Conversation
	for _, c := range m.convs {
		if c.RequesterID == userID || c.CourierID == userID {
			if errandIDFilter == nil || c.ErrandID == *errandIDFilter {
				list = append(list, c)
			}
		}
	}
	return list, nil
}

func (m *inMemoryChatRepo) ListAllConversationsAdmin(ctx context.Context, errandIDFilter *uuid.UUID, limit int) ([]*Conversation, error) {
	var list []*Conversation
	for _, c := range m.convs {
		if errandIDFilter == nil || c.ErrandID == *errandIDFilter {
			list = append(list, c)
		}
	}
	return list, nil
}

func (m *inMemoryChatRepo) UpsertConversationOnAccepted(ctx context.Context, errandID, requesterID, courierID uuid.UUID, occurredAt time.Time) (*Conversation, error) {
	for _, c := range m.convs {
		if c.ErrandID == errandID && c.CourierID == courierID {
			return c, nil
		}
	}
	conv := &Conversation{
		ID:                  uuid.New(),
		ErrandID:            errandID,
		RequesterID:         requesterID,
		CourierID:           courierID,
		Status:              StatusActive,
		LastEventOccurredAt: occurredAt,
		CreatedAt:           occurredAt,
		UpdatedAt:           occurredAt,
	}
	m.convs[conv.ID] = conv
	return conv, nil
}

func (m *inMemoryChatRepo) FreezeConversation(ctx context.Context, errandID, courierID uuid.UUID, occurredAt time.Time) error {
	for _, c := range m.convs {
		if c.ErrandID == errandID && c.CourierID == courierID {
			c.Status = StatusFrozen
			c.UpdatedAt = occurredAt
		}
	}
	return nil
}

func (m *inMemoryChatRepo) MarkErrandCompleted(ctx context.Context, errandID uuid.UUID, occurredAt time.Time) error {
	for _, c := range m.convs {
		if c.ErrandID == errandID {
			c.ErrandCompletedAt = &occurredAt
			c.UpdatedAt = occurredAt
		}
	}
	return nil
}

func (m *inMemoryChatRepo) FreezeErrandCancelledOrExpired(ctx context.Context, errandID uuid.UUID, occurredAt time.Time) error {
	for _, c := range m.convs {
		if c.ErrandID == errandID {
			c.Status = StatusFrozen
			c.UpdatedAt = occurredAt
		}
	}
	return nil
}

func (m *inMemoryChatRepo) FreezeConversationsPast24Hours(ctx context.Context, cutoff time.Time) (int64, error) {
	var count int64
	for _, c := range m.convs {
		if c.ErrandCompletedAt != nil && c.ErrandCompletedAt.Before(cutoff) && c.Status != StatusFrozen {
			c.Status = StatusFrozen
			count++
		}
	}
	return count, nil
}

func (m *inMemoryChatRepo) SaveMessage(ctx context.Context, msg *Message) (*Message, bool, error) {
	if msg.ClientMessageID != nil {
		for _, existing := range m.messages {
			if existing.ConversationID == msg.ConversationID &&
				existing.ClientMessageID != nil &&
				*existing.ClientMessageID == *msg.ClientMessageID {
				return existing, true, nil
			}
		}
	}
	m.messages = append(m.messages, msg)
	return msg, false, nil
}

func (m *inMemoryChatRepo) GetMessage(ctx context.Context, messageID uuid.UUID) (*Message, error) {
	for _, msg := range m.messages {
		if msg.ID == messageID {
			return msg, nil
		}
	}
	return nil, nil
}

func (m *inMemoryChatRepo) ListMessages(ctx context.Context, conversationID uuid.UUID, cursor *MessageCursor, since *time.Time, limit int) ([]*Message, *MessageCursor, bool, error) {
	var list []*Message
	for _, msg := range m.messages {
		if msg.ConversationID == conversationID {
			list = append(list, msg)
		}
	}
	return list, nil, false, nil
}

func (m *inMemoryChatRepo) UpdateLastRead(ctx context.Context, conversationID, userID uuid.UUID, readAt time.Time) error {
	key := conversationID.String() + "_" + userID.String()
	m.lastReadTimes[key] = readAt
	return nil
}

func (m *inMemoryChatRepo) GetUnreadSummary(ctx context.Context, userID uuid.UUID) (*UnreadSummary, error) {
	return &UnreadSummary{
		HasUnread:          false,
		UnreadErrandIDs:    []uuid.UUID{},
		RequesterHasUnread: false,
		CourierHasUnread:   false,
		RequesterErrandIDs: []uuid.UUID{},
		CourierErrandIDs:   []uuid.UUID{},
	}, nil
}

func (m *inMemoryChatRepo) RecordAudit(ctx context.Context, entry *AuditEntry) error {
	m.auditLogs = append(m.auditLogs, entry)
	return nil
}

type fakeBroadcaster struct {
	messages []*Message
	readOnly []uuid.UUID
}

func (b *fakeBroadcaster) BroadcastMessage(ctx context.Context, msg *Message, recipientID uuid.UUID) error {
	b.messages = append(b.messages, msg)
	return nil
}

func (b *fakeBroadcaster) BroadcastReadOnly(ctx context.Context, conversationID, errandID, requesterID, courierID uuid.UUID) error {
	b.readOnly = append(b.readOnly, conversationID)
	return nil
}

func (b *fakeBroadcaster) BroadcastReadReceipt(ctx context.Context, conversationID, readerID, recipientID uuid.UUID, readAt time.Time) error {
	return nil
}

type fixedClock struct {
	now time.Time
}

func (f fixedClock) Now() time.Time {
	return f.now
}

func TestService_SendMessage_Idempotency(t *testing.T) {
	repo := newInMemoryRepo()
	broadcaster := &fakeBroadcaster{}
	baseTime := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	clock := fixedClock{now: baseTime}
	svc := NewService(repo, broadcaster, clock)

	errandID := uuid.New()
	requesterID := uuid.New()
	courierID := uuid.New()

	conv, err := svc.CreateOrResumeConversation(context.Background(), errandID, requesterID, courierID, baseTime)
	require.NoError(t, err)

	clientMsgID := uuid.New()
	text := "Hello World!"

	// First send
	msg1, err := svc.SendTextMessage(context.Background(), requesterID, conv.ID, clientMsgID, text)
	require.NoError(t, err)
	assert.Equal(t, clientMsgID, *msg1.ClientMessageID)
	assert.Equal(t, text, *msg1.TextContent)
	assert.Len(t, broadcaster.messages, 1)

	// Resend with same client_message_id (idempotent retry)
	msg2, err := svc.SendTextMessage(context.Background(), requesterID, conv.ID, clientMsgID, text)
	require.NoError(t, err)
	assert.Equal(t, msg1.ID, msg2.ID)
	// Broadcaster should NOT have sent a duplicate event
	assert.Len(t, broadcaster.messages, 1)
}

func TestService_ReadOnly_After24Hours(t *testing.T) {
	repo := newInMemoryRepo()
	baseTime := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	clock := fixedClock{now: baseTime}
	svc := NewService(repo, nil, clock)

	errandID := uuid.New()
	requesterID := uuid.New()
	courierID := uuid.New()

	conv, err := svc.CreateOrResumeConversation(context.Background(), errandID, requesterID, courierID, baseTime)
	require.NoError(t, err)

	// Errand completed at baseTime
	err = svc.MarkErrandCompleted(context.Background(), errandID, baseTime)
	require.NoError(t, err)

	// At 23 hours post-completion, sending message should succeed
	svc23h := NewService(repo, nil, fixedClock{now: baseTime.Add(23 * time.Hour)})
	_, err = svc23h.SendTextMessage(context.Background(), requesterID, conv.ID, uuid.New(), "Still can send at 23h")
	require.NoError(t, err)

	// At 25 hours post-completion, sending message must fail with ErrConversationReadOnly
	svc25h := NewService(repo, nil, fixedClock{now: baseTime.Add(25 * time.Hour)})
	_, err = svc25h.SendTextMessage(context.Background(), requesterID, conv.ID, uuid.New(), "Too late")
	assert.ErrorIs(t, err, ErrConversationReadOnly)
}

func TestService_MarkCourierWithdrawn_FreezesConversation(t *testing.T) {
	repo := newInMemoryRepo()
	broadcaster := &fakeBroadcaster{}
	baseTime := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	svc := NewService(repo, broadcaster, fixedClock{now: baseTime})

	errandID := uuid.New()
	requesterID := uuid.New()
	courierID := uuid.New()

	conv, err := svc.CreateOrResumeConversation(context.Background(), errandID, requesterID, courierID, baseTime)
	require.NoError(t, err)

	// Courier withdraws
	err = svc.MarkCourierWithdrawn(context.Background(), errandID, courierID, baseTime.Add(time.Minute))
	require.NoError(t, err)

	assert.Len(t, broadcaster.readOnly, 1)

	// Attempting to send message should fail
	_, err = svc.SendTextMessage(context.Background(), courierID, conv.ID, uuid.New(), "hello after withdrawal")
	assert.ErrorIs(t, err, ErrConversationReadOnly)
}

func TestService_AdminAuditLogging(t *testing.T) {
	repo := newInMemoryRepo()
	baseTime := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	svc := NewService(repo, nil, fixedClock{now: baseTime})

	conv, err := svc.CreateOrResumeConversation(context.Background(), uuid.New(), uuid.New(), uuid.New(), baseTime)
	require.NoError(t, err)

	admin := Caller{UserID: uuid.New(), Role: RoleAdmin}
	convs, err := svc.AdminListConversations(context.Background(), admin, nil, 10)
	require.NoError(t, err)
	assert.Len(t, convs, 1)

	// Audit entry must be recorded per D-106
	require.Len(t, repo.auditLogs, 1)
	assert.Equal(t, admin.UserID, repo.auditLogs[0].AdminID)
	assert.Equal(t, conv.ID, repo.auditLogs[0].ConversationID)
	assert.Equal(t, AuditActionInspect, repo.auditLogs[0].Action)
}

func TestService_ValidateUnicodeMessageLength(t *testing.T) {
	// 250 runes with multi-byte emojis
	text250 := strings.Repeat("🎉", 250)
	assert.NoError(t, ValidateTextMessage(text250))

	// 251 runes rejected
	text251 := strings.Repeat("🎉", 251)
	assert.ErrorIs(t, ValidateTextMessage(text251), ErrTextTooLong)
}

func TestService_ListConversations_AdminCanViewAllConversations(t *testing.T) {
	repo := newInMemoryRepo()
	baseTime := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	svc := NewService(repo, nil, fixedClock{now: baseTime})

	errandID := uuid.New()
	_, err := svc.CreateOrResumeConversation(context.Background(), errandID, uuid.New(), uuid.New(), baseTime)
	require.NoError(t, err)

	admin := Caller{UserID: uuid.New(), Role: RoleAdmin}
	convs, err := svc.ListConversations(context.Background(), admin, &errandID)
	require.NoError(t, err)
	assert.Len(t, convs, 1)
}

func TestService_GetUnreadSummary(t *testing.T) {
	repo := newInMemoryRepo()
	svc := NewService(repo, nil, nil)
	userID := uuid.New()

	summary, err := svc.GetUnreadSummary(context.Background(), userID)
	require.NoError(t, err)
	assert.False(t, summary.HasUnread)
	assert.Empty(t, summary.UnreadErrandIDs)
	assert.False(t, summary.RequesterHasUnread)
	assert.False(t, summary.CourierHasUnread)
}
