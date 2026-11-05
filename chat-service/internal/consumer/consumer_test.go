package consumer

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"foc/chat-service/internal/chat"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeChatRepo struct {
	chat.Repository
	acceptedCalled  bool
	withdrawnCalled bool
	completedCalled bool
	terminatedCalled bool
}

func (f *fakeChatRepo) UpsertConversationOnAccepted(ctx context.Context, errandID, requesterID, courierID uuid.UUID, occurredAt time.Time) (*chat.Conversation, error) {
	f.acceptedCalled = true
	return &chat.Conversation{
		ID:          uuid.New(),
		ErrandID:    errandID,
		RequesterID: requesterID,
		CourierID:   courierID,
		Status:      chat.StatusActive,
	}, nil
}

func (f *fakeChatRepo) GetConversationByErrandCourier(ctx context.Context, errandID, courierID uuid.UUID) (*chat.Conversation, error) {
	return &chat.Conversation{
		ID:          uuid.New(),
		ErrandID:    errandID,
		CourierID:   courierID,
		RequesterID: uuid.New(),
		Status:      chat.StatusActive,
	}, nil
}

func (f *fakeChatRepo) FreezeConversation(ctx context.Context, errandID, courierID uuid.UUID, occurredAt time.Time) error {
	f.withdrawnCalled = true
	return nil
}

func (f *fakeChatRepo) MarkErrandCompleted(ctx context.Context, errandID uuid.UUID, occurredAt time.Time) error {
	f.completedCalled = true
	return nil
}

func (f *fakeChatRepo) FreezeErrandCancelledOrExpired(ctx context.Context, errandID uuid.UUID, occurredAt time.Time) error {
	f.terminatedCalled = true
	return nil
}

func TestLifecycleHandler_Routing(t *testing.T) {
	errandID := uuid.New()
	requesterID := uuid.New()
	courierID := uuid.New()
	now := time.Now().UTC()

	tests := []struct {
		name       string
		routingKey string
		payload    ErrandLifecycleEvent
		check      func(t *testing.T, repo *fakeChatRepo)
	}{
		{
			name:       "errand.accepted",
			routingKey: "errand.accepted",
			payload: ErrandLifecycleEvent{
				EventID:     uuid.New(),
				EventType:   "errand.accepted",
				ErrandID:    errandID,
				RequesterID: requesterID,
				CourierID:   &courierID,
				OccurredAt:  now,
			},
			check: func(t *testing.T, repo *fakeChatRepo) {
				assert.True(t, repo.acceptedCalled)
			},
		},
		{
			name:       "errand.withdrawn",
			routingKey: "errand.withdrawn",
			payload: ErrandLifecycleEvent{
				EventID:    uuid.New(),
				EventType:  "errand.withdrawn",
				ErrandID:   errandID,
				CourierID:  &courierID,
				OccurredAt: now,
			},
			check: func(t *testing.T, repo *fakeChatRepo) {
				assert.True(t, repo.withdrawnCalled)
			},
		},
		{
			name:       "errand.completed",
			routingKey: "errand.completed",
			payload: ErrandLifecycleEvent{
				EventID:    uuid.New(),
				EventType:  "errand.completed",
				ErrandID:   errandID,
				OccurredAt: now,
			},
			check: func(t *testing.T, repo *fakeChatRepo) {
				assert.True(t, repo.completedCalled)
			},
		},
		{
			name:       "errand.cancelled",
			routingKey: "errand.cancelled",
			payload: ErrandLifecycleEvent{
				EventID:    uuid.New(),
				EventType:  "errand.cancelled",
				ErrandID:   errandID,
				OccurredAt: now,
			},
			check: func(t *testing.T, repo *fakeChatRepo) {
				assert.True(t, repo.terminatedCalled)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeChatRepo{}
			svc := chat.NewService(repo, nil, nil)
			handler := NewLifecycleHandler(svc)

			body, err := json.Marshal(tc.payload)
			require.NoError(t, err)

			err = handler.Handle(context.Background(), tc.routingKey, body)
			require.NoError(t, err)
			tc.check(t, repo)
		})
	}
}
