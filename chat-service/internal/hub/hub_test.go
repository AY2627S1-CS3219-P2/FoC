package hub

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

func TestHub_LocalDispatch(t *testing.T) {
	h := NewHub(nil, nil, nil)
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
