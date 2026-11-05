package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"foc/chat-service/internal/auth"
	"foc/chat-service/internal/chat"
	"foc/chat-service/internal/media"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockRepo struct {
	chat.Repository
	convs    map[uuid.UUID]*chat.Conversation
	messages map[uuid.UUID]*chat.Message
}

func newMockRepo() *mockRepo {
	return &mockRepo{
		convs:    make(map[uuid.UUID]*chat.Conversation),
		messages: make(map[uuid.UUID]*chat.Message),
	}
}

func (m *mockRepo) GetConversation(ctx context.Context, id uuid.UUID) (*chat.Conversation, error) {
	c, ok := m.convs[id]
	if !ok {
		return nil, chat.ErrConversationNotFound
	}
	return c, nil
}

func (m *mockRepo) ListConversationsByUser(ctx context.Context, userID uuid.UUID, errandIDFilter *uuid.UUID) ([]*chat.Conversation, error) {
	var list []*chat.Conversation
	for _, c := range m.convs {
		if c.RequesterID == userID || c.CourierID == userID {
			if errandIDFilter == nil || c.ErrandID == *errandIDFilter {
				list = append(list, c)
			}
		}
	}
	return list, nil
}

func (m *mockRepo) ListAllConversationsAdmin(ctx context.Context, errandIDFilter *uuid.UUID, limit int) ([]*chat.Conversation, error) {
	var list []*chat.Conversation
	for _, c := range m.convs {
		if errandIDFilter == nil || c.ErrandID == *errandIDFilter {
			list = append(list, c)
		}
	}
	return list, nil
}

func (m *mockRepo) RecordAudit(ctx context.Context, entry *chat.AuditEntry) error {
	return nil
}

func (m *mockRepo) SaveMessage(ctx context.Context, msg *chat.Message) (*chat.Message, bool, error) {
	m.messages[msg.ID] = msg
	return msg, false, nil
}

func (m *mockRepo) ListMessages(ctx context.Context, conversationID uuid.UUID, cursor *chat.MessageCursor, since *time.Time, limit int) ([]*chat.Message, *chat.MessageCursor, bool, error) {
	var list []*chat.Message
	for _, msg := range m.messages {
		if msg.ConversationID == conversationID {
			list = append(list, msg)
		}
	}
	return list, nil, false, nil
}

func (m *mockRepo) UpdateLastRead(ctx context.Context, conversationID, userID uuid.UUID, readAt time.Time) error {
	return nil
}

func (m *mockRepo) GetUnreadSummary(ctx context.Context, userID uuid.UUID) (bool, []uuid.UUID, error) {
	return false, []uuid.UUID{}, nil
}

func setupTestServer(t *testing.T, repo chat.Repository) (*Handler, func()) {
	tmpDir, err := os.MkdirTemp("", "chat-media-test-*")
	require.NoError(t, err)

	mediaStore, err := media.NewStorage(tmpDir)
	require.NoError(t, err)

	svc := chat.NewService(repo, nil, nil)
	handler := NewHandler(svc, nil, mediaStore, slog.New(slog.DiscardHandler))

	cleanup := func() {
		_ = os.RemoveAll(tmpDir)
	}
	return handler, cleanup
}

func TestHealthCheck(t *testing.T) {
	handler, cleanup := setupTestServer(t, newMockRepo())
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.HealthCheck(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"status":"ok"`)
}

func TestGetConversation_NotFound(t *testing.T) {
	handler, cleanup := setupTestServer(t, newMockRepo())
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/conversations/"+uuid.New().String(), nil)
	caller := chat.Caller{UserID: uuid.New(), Role: "STUDENT"}
	req = req.WithContext(auth.WithCaller(req.Context(), caller))

	r := chi.NewRouter()
	r.Get("/conversations/{conversationId}", handler.GetConversation)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestGetConversation_Authorized(t *testing.T) {
	repo := newMockRepo()
	convID := uuid.New()
	userID := uuid.New()
	repo.convs[convID] = &chat.Conversation{
		ID:          convID,
		ErrandID:    uuid.New(),
		RequesterID: userID,
		CourierID:   uuid.New(),
		Status:      chat.StatusActive,
	}

	handler, cleanup := setupTestServer(t, repo)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/conversations/"+convID.String(), nil)
	caller := chat.Caller{UserID: userID, Role: "STUDENT"}
	req = req.WithContext(auth.WithCaller(req.Context(), caller))

	r := chi.NewRouter()
	r.Get("/conversations/{conversationId}", handler.GetConversation)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp chat.Conversation
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, convID, resp.ID)
}

func TestSendMessage_Text(t *testing.T) {
	repo := newMockRepo()
	convID := uuid.New()
	userID := uuid.New()
	courierID := uuid.New()
	repo.convs[convID] = &chat.Conversation{
		ID:          convID,
		ErrandID:    uuid.New(),
		RequesterID: userID,
		CourierID:   courierID,
		Status:      chat.StatusActive,
	}

	handler, cleanup := setupTestServer(t, repo)
	defer cleanup()

	payload := map[string]string{
		"client_message_id": uuid.New().String(),
		"text":              "Hello Courier! 🛵",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/conversations/"+convID.String()+"/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	caller := chat.Caller{UserID: userID, Role: "STUDENT"}
	req = req.WithContext(auth.WithCaller(req.Context(), caller))

	r := chi.NewRouter()
	r.Post("/conversations/{conversationId}/messages", handler.SendMessage)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var msg chat.Message
	err := json.Unmarshal(rec.Body.Bytes(), &msg)
	require.NoError(t, err)
	assert.Equal(t, "Hello Courier! 🛵", *msg.TextContent)
}

func TestSendMessage_ValidationRejection(t *testing.T) {
	repo := newMockRepo()
	convID := uuid.New()
	userID := uuid.New()
	repo.convs[convID] = &chat.Conversation{
		ID:          convID,
		ErrandID:    uuid.New(),
		RequesterID: userID,
		CourierID:   uuid.New(),
		Status:      chat.StatusActive,
	}

	handler, cleanup := setupTestServer(t, repo)
	defer cleanup()

	// Empty text
	payload := map[string]string{
		"client_message_id": uuid.New().String(),
		"text":              "   ",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/conversations/"+convID.String()+"/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	caller := chat.Caller{UserID: userID, Role: "STUDENT"}
	req = req.WithContext(auth.WithCaller(req.Context(), caller))

	r := chi.NewRouter()
	r.Post("/conversations/{conversationId}/messages", handler.SendMessage)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAdminListConversations_ForbiddenForStudent(t *testing.T) {
	handler, cleanup := setupTestServer(t, newMockRepo())
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/admin/conversations", nil)
	caller := chat.Caller{UserID: uuid.New(), Role: "STUDENT"}
	req = req.WithContext(auth.WithCaller(req.Context(), caller))

	rec := httptest.NewRecorder()
	handler.AdminListConversations(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}
