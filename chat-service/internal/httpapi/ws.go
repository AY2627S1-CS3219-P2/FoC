package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"foc/chat-service/internal/hub"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/websocket"
)

// ServeWS handles WebSocket connection upgrades authenticated via single-use ticket.
func (h *Handler) ServeWS(wsHub *hub.Hub) http.HandlerFunc {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  64 * 1024,
		WriteBufferSize: 64 * 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true // Allow cross-origin WebSocket connections behind API Gateway
		},
		Error: h.refuseUpgrade,
	}
	return func(w http.ResponseWriter, r *http.Request) {
		ticketStr := r.URL.Query().Get("ticket")
		if ticketStr == "" {
			writeError(w, http.StatusUnauthorized, "missing ticket parameter")
			return
		}

		userID, err := h.ticketStore.RedeemTicket(r.Context(), ticketStr)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid, expired, or already redeemed ticket")
			return
		}
		recordCaller(r.Context(), userID)

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			// refuseUpgrade has logged a refused handshake; any other error
			// came after the connection was taken over, too late for a status.
			if !errors.As(err, new(websocket.HandshakeError)) {
				h.log.WarnContext(r.Context(), "websocket upgrade failed",
					"request_id", middleware.GetReqID(r.Context()), "user_id", userID.String(), "err", err)
			}
			return
		}

		client := hub.NewClient(wsHub, conn, userID)
		wsHub.RegisterClient(client)

		go client.WritePump()
		go client.ReadPump()
	}
}

// refuseUpgrade answers a refused handshake as gorilla/websocket does by
// default and logs why: at ERROR, as the cause of a 5xx, when the fault is
// this service's, and at WARN when the client's handshake was bad.
func (h *Handler) refuseUpgrade(w http.ResponseWriter, r *http.Request, status int, reason error) {
	level := slog.LevelWarn
	if status >= http.StatusInternalServerError && !hungUp(r) {
		level = slog.LevelError
	}
	h.log.LogAttrs(r.Context(), level, "websocket upgrade refused",
		slog.String("request_id", middleware.GetReqID(r.Context())),
		slog.Int("status", status),
		slog.String("err", reason.Error()))
	w.Header().Set("Sec-Websocket-Version", "13")
	http.Error(w, http.StatusText(status), status)
}
