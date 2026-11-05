package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"foc/chat-service/internal/auth"
	"foc/chat-service/internal/chat"
	"foc/chat-service/internal/media"
	"foc/chat-service/internal/ticket"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type Handler struct {
	service      *chat.Service
	ticketStore  *ticket.Store
	mediaStorage *media.Storage
	log          *slog.Logger
}

func NewHandler(service *chat.Service, ticketStore *ticket.Store, mediaStorage *media.Storage, log *slog.Logger) *Handler {
	return &Handler{
		service:      service,
		ticketStore:  ticketStore,
		mediaStorage: mediaStorage,
		log:          log,
	}
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// IssueWSTicket issues a single-use 30s ticket for WebSocket authentication.
func (h *Handler) IssueWSTicket(w http.ResponseWriter, r *http.Request) {
	caller, ok := auth.CallerFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	t, err := h.ticketStore.IssueTicket(r.Context(), caller.UserID)
	if err != nil {
		h.serverError(w, r, "failed to issue ticket", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"ticket": t})
}

// ListConversations lists conversations for caller, optionally filtered by errandId.
func (h *Handler) ListConversations(w http.ResponseWriter, r *http.Request) {
	caller, ok := auth.CallerFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var errandFilter *uuid.UUID
	if errandStr := r.URL.Query().Get("errandId"); errandStr != "" {
		id, err := uuid.Parse(errandStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid errandId query param")
			return
		}
		errandFilter = &id
	}

	convs, err := h.service.ListConversations(r.Context(), caller, errandFilter)
	if err != nil {
		h.serverError(w, r, err.Error(), err)
		return
	}
	if convs == nil {
		convs = []*chat.Conversation{}
	}
	writeJSON(w, http.StatusOK, convs)
}

// GetConversation fetches a single conversation by ID.
func (h *Handler) GetConversation(w http.ResponseWriter, r *http.Request) {
	caller, ok := auth.CallerFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	convIDStr := chi.URLParam(r, "conversationId")
	convID, err := uuid.Parse(convIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid conversationId")
		return
	}

	conv, err := h.service.GetConversation(r.Context(), caller, convID)
	if err != nil {
		if errors.Is(err, chat.ErrConversationNotFound) {
			writeError(w, http.StatusNotFound, "conversation not found")
			return
		}
		if errors.Is(err, chat.ErrUnauthorized) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		h.serverError(w, r, err.Error(), err)
		return
	}

	writeJSON(w, http.StatusOK, conv)
}

// GetMessages lists paginated messages in a conversation.
func (h *Handler) GetMessages(w http.ResponseWriter, r *http.Request) {
	caller, ok := auth.CallerFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	convIDStr := chi.URLParam(r, "conversationId")
	convID, err := uuid.Parse(convIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid conversationId")
		return
	}

	limit := 50
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	var cursor *chat.MessageCursor
	if cursorStr := r.URL.Query().Get("cursor"); cursorStr != "" {
		c, err := chat.ParseCursor(cursorStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		cursor = c
	}

	var since *time.Time
	if sinceStr := r.URL.Query().Get("since"); sinceStr != "" {
		t, err := time.Parse(time.RFC3339Nano, sinceStr)
		if err != nil {
			t, err = time.Parse(time.RFC3339, sinceStr)
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid since timestamp")
			return
		}
		since = &t
	}

	page, err := h.service.GetMessages(r.Context(), caller, convID, cursor, since, limit)
	if err != nil {
		if errors.Is(err, chat.ErrConversationNotFound) {
			writeError(w, http.StatusNotFound, "conversation not found")
			return
		}
		if errors.Is(err, chat.ErrUnauthorized) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		h.serverError(w, r, err.Error(), err)
		return
	}
	if page.Messages == nil {
		page.Messages = []*chat.Message{}
	}

	writeJSON(w, http.StatusOK, page)
}

type sendTextRequest struct {
	ClientMessageID string `json:"client_message_id"`
	Text            string `json:"text"`
}

// SendMessage creates a text or image message idempotently.
func (h *Handler) SendMessage(w http.ResponseWriter, r *http.Request) {
	caller, ok := auth.CallerFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	convIDStr := chi.URLParam(r, "conversationId")
	convID, err := uuid.Parse(convIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid conversationId")
		return
	}

	contentType := r.Header.Get("Content-Type")

	// Multipart Image Upload
	if strings.HasPrefix(contentType, "multipart/form-data") {
		err := r.ParseMultipartForm(11 * 1024 * 1024)
		if err != nil {
			writeError(w, http.StatusBadRequest, "malformed multipart form")
			return
		}

		clientMsgIDStr := r.FormValue("client_message_id")
		clientMsgID, err := uuid.Parse(clientMsgIDStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid client_message_id")
			return
		}

		file, header, err := r.FormFile("image")
		if err != nil {
			writeError(w, http.StatusBadRequest, "missing image file in form-data")
			return
		}
		defer func() { _ = file.Close() }()

		if header.Size > chat.MaxImageSizeBytes {
			writeError(w, http.StatusRequestEntityTooLarge, "image exceeds 10MB limit")
			return
		}

		mediaID := uuid.New()
		_, err = h.mediaStorage.Store(r.Context(), mediaID, file)
		if err != nil {
			if errors.Is(err, chat.ErrUnsupportedImageType) || errors.Is(err, media.ErrInvalidMedia) {
				writeError(w, http.StatusBadRequest, "unsupported image type; only JPEG and PNG are allowed")
				return
			}
			h.serverError(w, r, fmt.Sprintf("failed to store image: %v", err), err)
			return
		}

		mediaURL := fmt.Sprintf("/api/chat/images/%s", mediaID.String())
		msg, err := h.service.SendImageMessage(r.Context(), caller.UserID, convID, clientMsgID, mediaID, mediaURL)
		if err != nil {
			// Rollback stored file if DB insertion failed
			_ = h.mediaStorage.Delete(r.Context(), mediaID)
			h.mapServiceError(w, r, err)
			return
		}

		writeJSON(w, http.StatusOK, msg)
		return
	}

	// JSON Text Message
	var req sendTextRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json payload")
		return
	}

	clientMsgID, err := uuid.Parse(req.ClientMessageID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid client_message_id")
		return
	}

	msg, err := h.service.SendTextMessage(r.Context(), caller.UserID, convID, clientMsgID, req.Text)
	if err != nil {
		h.mapServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, msg)
}

func (h *Handler) mapServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, chat.ErrEmptyMessage), errors.Is(err, chat.ErrTextTooLong):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, chat.ErrImageTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, err.Error())
	case errors.Is(err, chat.ErrUnsupportedImageType):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, chat.ErrConversationReadOnly):
		writeError(w, 423, "conversation is read-only") // 423 Locked
	case errors.Is(err, chat.ErrUnauthorized):
		writeError(w, http.StatusForbidden, "forbidden")
	case errors.Is(err, chat.ErrConversationNotFound):
		writeError(w, http.StatusNotFound, "conversation not found")
	default:
		h.serverError(w, r, err.Error(), err)
	}
}

// DownloadImage streams stored image bytes.
func (h *Handler) DownloadImage(w http.ResponseWriter, r *http.Request) {
	imageIDStr := chi.URLParam(r, "imageId")
	if imageIDStr == "" {
		imageIDStr = chi.URLParam(r, "mediaId")
	}
	imageID, err := uuid.Parse(imageIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid imageId")
		return
	}

	reader, mimeType, err := h.mediaStorage.Open(r.Context(), imageID)
	if err != nil {
		writeError(w, http.StatusNotFound, "image not found")
		return
	}
	defer func() { _ = reader.Close() }()

	w.Header().Set("Content-Type", mimeType)
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, reader)
}

// MarkRead marks conversation read up to now.
func (h *Handler) MarkRead(w http.ResponseWriter, r *http.Request) {
	caller, ok := auth.CallerFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	convIDStr := chi.URLParam(r, "conversationId")
	convID, err := uuid.Parse(convIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid conversationId")
		return
	}

	err = h.service.MarkAsRead(r.Context(), caller.UserID, convID)
	if err != nil {
		if errors.Is(err, chat.ErrConversationNotFound) {
			writeError(w, http.StatusNotFound, "conversation not found")
			return
		}
		if errors.Is(err, chat.ErrUnauthorized) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		h.serverError(w, r, err.Error(), err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"last_read_at": time.Now().UTC().Format(time.RFC3339Nano),
	})
}

// GetUnreadSummary returns boolean and list of unread errand IDs.
func (h *Handler) GetUnreadSummary(w http.ResponseWriter, r *http.Request) {
	caller, ok := auth.CallerFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	hasUnread, unreadErrands, err := h.service.GetUnreadSummary(r.Context(), caller.UserID)
	if err != nil {
		h.serverError(w, r, err.Error(), err)
		return
	}
	if unreadErrands == nil {
		unreadErrands = []uuid.UUID{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"has_unread":        hasUnread,
		"unread_errand_ids": unreadErrands,
	})
}

// AdminListConversations provides admin audit-tracked inspection of conversations.
func (h *Handler) AdminListConversations(w http.ResponseWriter, r *http.Request) {
	caller, ok := auth.CallerFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if caller.Role != chat.RoleAdmin {
		writeError(w, http.StatusForbidden, "forbidden: admin role required")
		return
	}

	var errandFilter *uuid.UUID
	if errandStr := r.URL.Query().Get("errandId"); errandStr != "" {
		id, err := uuid.Parse(errandStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid errandId query param")
			return
		}
		errandFilter = &id
	}

	limit := 50
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	convs, err := h.service.AdminListConversations(r.Context(), caller, errandFilter, limit)
	if err != nil {
		h.serverError(w, r, err.Error(), err)
		return
	}
	if convs == nil {
		convs = []*chat.Conversation{}
	}

	writeJSON(w, http.StatusOK, convs)
}

// HealthCheck reports service status.
func (h *Handler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
