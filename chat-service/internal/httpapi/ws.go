package httpapi

import (
	"log"
	"net/http"

	"foc/chat-service/internal/hub"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  64 * 1024,
	WriteBufferSize: 64 * 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow cross-origin WebSocket connections behind API Gateway
	},
}

// ServeWS handles WebSocket connection upgrades authenticated via single-use ticket.
func (h *Handler) ServeWS(wsHub *hub.Hub) http.HandlerFunc {
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

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("websocket upgrade error: %v", err)
			return
		}

		client := hub.NewClient(wsHub, conn, userID)
		wsHub.RegisterClient(client)

		go client.WritePump()
		go client.ReadPump()
	}
}
