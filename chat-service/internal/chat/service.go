package chat

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	repo        Repository
	broadcaster EventBroadcaster
	clock       Clock
}

func NewService(repo Repository, broadcaster EventBroadcaster, clock Clock) *Service {
	if clock == nil {
		clock = RealClock{}
	}
	return &Service{
		repo:        repo,
		broadcaster: broadcaster,
		clock:       clock,
	}
}

func (s *Service) SendTextMessage(ctx context.Context, senderID, convID, clientMsgID uuid.UUID, text string) (*Message, error) {
	if err := ValidateTextMessage(text); err != nil {
		return nil, err
	}
	now := s.clock.Now()
	conv, err := s.repo.GetConversation(ctx, convID)
	if err != nil {
		return nil, err
	}
	if _, ok := conv.ParticipantRole(senderID); !ok {
		return nil, ErrUnauthorized
	}
	if conv.IsReadOnly(now) {
		return nil, ErrConversationReadOnly
	}

	msg := &Message{
		ID:              uuid.New(),
		ConversationID:  convID,
		SenderID:        senderID,
		Type:            MessageTypeText,
		TextContent:     &text,
		ClientMessageID: &clientMsgID,
		CreatedAt:       now,
	}

	saved, isDup, err := s.repo.SaveMessage(ctx, msg)
	if err != nil {
		return nil, fmt.Errorf("save text message: %w", err)
	}

	if !isDup && s.broadcaster != nil {
		if recipientID, ok := conv.OtherParticipantID(senderID); ok {
			_ = s.broadcaster.BroadcastMessage(ctx, saved, recipientID)
		}
	}

	return saved, nil
}

func (s *Service) SendImageMessage(ctx context.Context, senderID, convID, clientMsgID, mediaID uuid.UUID, mediaURL string) (*Message, error) {
	now := s.clock.Now()
	conv, err := s.repo.GetConversation(ctx, convID)
	if err != nil {
		return nil, err
	}
	if _, ok := conv.ParticipantRole(senderID); !ok {
		return nil, ErrUnauthorized
	}
	if conv.IsReadOnly(now) {
		return nil, ErrConversationReadOnly
	}

	msg := &Message{
		ID:              uuid.New(),
		ConversationID:  convID,
		SenderID:        senderID,
		Type:            MessageTypeImage,
		MediaID:         &mediaID,
		MediaURL:        &mediaURL,
		ClientMessageID: &clientMsgID,
		CreatedAt:       now,
	}

	saved, isDup, err := s.repo.SaveMessage(ctx, msg)
	if err != nil {
		return nil, fmt.Errorf("save image message: %w", err)
	}

	if !isDup && s.broadcaster != nil {
		if recipientID, ok := conv.OtherParticipantID(senderID); ok {
			_ = s.broadcaster.BroadcastMessage(ctx, saved, recipientID)
		}
	}

	return saved, nil
}

func (s *Service) GetConversation(ctx context.Context, caller Caller, convID uuid.UUID) (*Conversation, error) {
	conv, err := s.repo.GetConversation(ctx, convID)
	if err != nil {
		return nil, err
	}
	if caller.Role != RoleAdmin {
		if _, ok := conv.ParticipantRole(caller.UserID); !ok {
			return nil, ErrUnauthorized
		}
	}
	return conv, nil
}

func (s *Service) ListConversations(ctx context.Context, caller Caller, errandIDFilter *uuid.UUID) ([]*Conversation, error) {
	if caller.Role == RoleAdmin {
		return s.repo.ListAllConversationsAdmin(ctx, errandIDFilter, 50)
	}
	return s.repo.ListConversationsByUser(ctx, caller.UserID, errandIDFilter)
}

func (s *Service) GetMessages(ctx context.Context, caller Caller, convID uuid.UUID, cursor *MessageCursor, since *time.Time, limit int) (*MessagePage, error) {
	conv, err := s.repo.GetConversation(ctx, convID)
	if err != nil {
		return nil, err
	}
	if caller.Role != RoleAdmin {
		if _, ok := conv.ParticipantRole(caller.UserID); !ok {
			return nil, ErrUnauthorized
		}
	}

	if limit <= 0 || limit > 100 {
		limit = 50
	}

	messages, nextCursor, hasMore, err := s.repo.ListMessages(ctx, convID, cursor, since, limit)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}

	return &MessagePage{
		Messages:   messages,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}

func (s *Service) MarkAsRead(ctx context.Context, callerID, convID uuid.UUID) error {
	now := s.clock.Now()
	conv, err := s.repo.GetConversation(ctx, convID)
	if err != nil {
		return err
	}
	if _, ok := conv.ParticipantRole(callerID); !ok {
		return ErrUnauthorized
	}
	if err := s.repo.UpdateLastRead(ctx, convID, callerID, now); err != nil {
		return err
	}
	if s.broadcaster != nil {
		if otherID, ok := conv.OtherParticipantID(callerID); ok {
			_ = s.broadcaster.BroadcastReadReceipt(ctx, convID, callerID, otherID, now)
		}
	}
	return nil
}

func (s *Service) GetUnreadSummary(ctx context.Context, callerID uuid.UUID) (bool, []uuid.UUID, error) {
	return s.repo.GetUnreadSummary(ctx, callerID)
}

func (s *Service) AdminListConversations(ctx context.Context, admin Caller, errandIDFilter *uuid.UUID, limit int) ([]*Conversation, error) {
	if admin.Role != RoleAdmin {
		return nil, ErrForbidden
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	convs, err := s.repo.ListAllConversationsAdmin(ctx, errandIDFilter, limit)
	if err != nil {
		return nil, err
	}
	now := s.clock.Now()
	for _, conv := range convs {
		_ = s.repo.RecordAudit(ctx, &AuditEntry{
			ID:             uuid.New(),
			AdminID:        admin.UserID,
			Action:         AuditActionInspect,
			ConversationID: conv.ID,
			ErrandID:       conv.ErrandID,
			OccurredAt:     now,
		})
	}
	return convs, nil
}

func (s *Service) CreateOrResumeConversation(ctx context.Context, errandID, requesterID, courierID uuid.UUID, occurredAt time.Time) (*Conversation, error) {
	return s.repo.UpsertConversationOnAccepted(ctx, errandID, requesterID, courierID, occurredAt)
}

func (s *Service) MarkCourierWithdrawn(ctx context.Context, errandID, courierID uuid.UUID, occurredAt time.Time) error {
	conv, err := s.repo.GetConversationByErrandCourier(ctx, errandID, courierID)
	if err == nil && conv != nil && s.broadcaster != nil {
		_ = s.broadcaster.BroadcastReadOnly(ctx, conv.ID, conv.ErrandID, conv.RequesterID, conv.CourierID)
	}
	return s.repo.FreezeConversation(ctx, errandID, courierID, occurredAt)
}

func (s *Service) MarkErrandCompleted(ctx context.Context, errandID uuid.UUID, occurredAt time.Time) error {
	return s.repo.MarkErrandCompleted(ctx, errandID, occurredAt)
}

func (s *Service) MarkErrandTerminated(ctx context.Context, errandID uuid.UUID, occurredAt time.Time) error {
	return s.repo.FreezeErrandCancelledOrExpired(ctx, errandID, occurredAt)
}

func (s *Service) FreezeExpiredCompletedConversations(ctx context.Context) (int64, error) {
	now := s.clock.Now()
	cutoff := now.Add(-24 * time.Hour)
	return s.repo.FreezeConversationsPast24Hours(ctx, cutoff)
}
