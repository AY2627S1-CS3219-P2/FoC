package httpapi

import (
	"net/http"

	"foc/chat-service/internal/auth"
	"foc/chat-service/internal/hub"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func NewRouter(h *Handler, wsHub *hub.Hub, verifier *auth.Verifier) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Health check endpoints
	r.Get("/health", h.HealthCheck)
	r.Get("/api/v1/health", h.HealthCheck)

	// Ticket-authenticated WebSocket upgrade endpoint
	r.Get("/ws", h.ServeWS(wsHub))
	r.Get("/api/chat/ws", h.ServeWS(wsHub))

	// Media/image downloads (loaded directly by browser <img> tags without auth headers)
	r.Get("/images/{imageId}", h.DownloadImage)
	r.Get("/media/{mediaId}", h.DownloadImage)
	r.Get("/api/chat/images/{imageId}", h.DownloadImage)
	r.Get("/api/chat/media/{mediaId}", h.DownloadImage)

	// Routes helper that registers protected endpoints on a router
	registerProtected := func(sub chi.Router) {
		sub.Post("/ws-ticket", h.IssueWSTicket)
		sub.Get("/conversations", h.ListConversations)
		sub.Get("/conversations/{conversationId}", h.GetConversation)
		sub.Get("/conversations/{conversationId}/messages", h.GetMessages)
		sub.Post("/conversations/{conversationId}/messages", h.SendMessage)
		sub.Post("/conversations/{conversationId}/read", h.MarkRead)
		sub.Get("/unread-summary", h.GetUnreadSummary)
		sub.Get("/admin/conversations", h.AdminListConversations)
	}

	// Protected routes under root (standard when gateway strips prefix)
	r.Group(func(sub chi.Router) {
		sub.Use(auth.RequireToken(verifier))
		registerProtected(sub)
	})

	// Also support under /api/chat prefix (in case gateway forwards unstripped)
	r.Route("/api/chat", func(sub chi.Router) {
		sub.Use(auth.RequireToken(verifier))
		registerProtected(sub)
	})

	return r
}
