package hub

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	"foc/chat-service/internal/chat"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHub_LocalDispatch(t *testing.T) {
	h := NewHub(nil, nil, nil, slog.New(slog.DiscardHandler))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go h.Run(ctx)

	userID := uuid.New()
	client := &Client{
		hub:    h,
		userID: userID,
		send:   make(chan []byte, 10),
	}

	h.RegisterClient(client)
	time.Sleep(20 * time.Millisecond)

	msgText := "hello from test"
	msg := &chat.Message{
		ID:          uuid.New(),
		SenderID:    uuid.New(),
		Type:        chat.MessageTypeText,
		TextContent: &msgText,
		CreatedAt:   time.Now().UTC(),
	}

	err := h.BroadcastMessage(context.Background(), msg, userID)
	require.NoError(t, err)

	select {
	case payload := <-client.send:
		var env RealtimeEnvelope
		err := json.Unmarshal(payload, &env)
		require.NoError(t, err)
		assert.Equal(t, "message.created", env.Type)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for message payload")
	}

	h.UnregisterClient(client)
	time.Sleep(20 * time.Millisecond)
}

func TestClient_AuthenticatedAt(t *testing.T) {
	userID := uuid.New()
	before := time.Now().UTC().Add(-time.Second)
	client := NewClient(nil, nil, userID)
	after := time.Now().UTC().Add(time.Second)

	authAt := client.getAuthenticatedAt()
	assert.True(t, authAt.After(before) && authAt.Before(after), "authenticatedAt should be set to current time")

	newTime := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	client.setAuthenticatedAt(newTime)
	assert.Equal(t, newTime, client.getAuthenticatedAt())

	// Test concurrent access
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = client.getAuthenticatedAt()
		}()
		go func(iter int) {
			defer wg.Done()
			client.setAuthenticatedAt(time.Now().UTC())
		}(i)
	}
	wg.Wait()
}

func TestSuspensionChecker_ReceivesAuthenticatedAt(t *testing.T) {
	userID := uuid.New()
	expectedTime := time.Date(2026, 11, 9, 10, 30, 0, 0, time.UTC)

	checked := false
	checker := func(ctx context.Context, uid uuid.UUID, authAt time.Time) (bool, error) {
		assert.Equal(t, userID, uid)
		assert.Equal(t, expectedTime, authAt)
		checked = true
		return false, nil
	}

	h := NewHub(nil, checker, nil, slog.New(slog.DiscardHandler))
	client := NewClient(h, nil, userID)
	client.setAuthenticatedAt(expectedTime)

	suspended, err := h.isSuspended(context.Background(), client.userID, client.getAuthenticatedAt())
	require.NoError(t, err)
	assert.False(t, suspended)
	assert.True(t, checked)
}
