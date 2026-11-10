package hub

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var errTokenOfAnotherUser = errors.New("token belongs to another user")

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = 30 * time.Second
	maxMessageSize = 64 * 1024
	sendBufferSize = 256
)

type ClientMessage struct {
	Type  string `json:"type"`
	Token string `json:"token,omitempty"`
}

type Client struct {
	hub             *Hub
	conn            *websocket.Conn
	userID          uuid.UUID
	mu              sync.RWMutex
	authenticatedAt time.Time
	send            chan []byte
}

func NewClient(hub *Hub, conn *websocket.Conn, userID uuid.UUID) *Client {
	return &Client{
		hub:             hub,
		conn:            conn,
		userID:          userID,
		authenticatedAt: time.Now().UTC(),
		send:            make(chan []byte, sendBufferSize),
	}
}

func (c *Client) getAuthenticatedAt() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.authenticatedAt.IsZero() {
		return time.Now().UTC()
	}
	return c.authenticatedAt
}

func (c *Client) setAuthenticatedAt(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.authenticatedAt = t
}

func (c *Client) ReadPump() {
	defer func() {
		c.hub.UnregisterClient(c)
		_ = c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			// A browser's ws.close() with no code arrives as 1005, so 1000 and
			// 1005 are routine closes too.
			if ce, ok := err.(*websocket.CloseError); ok && websocket.IsUnexpectedCloseError(ce, websocket.CloseNormalClosure, websocket.CloseNoStatusReceived, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				// The code only: the close reason is whatever text the client sent.
				c.hub.log.Warn("websocket read error", "user_id", c.userID.String(), "close_code", ce.Code)
			}
			break
		}

		var clientMsg ClientMessage
		if err := json.Unmarshal(message, &clientMsg); err == nil {
			if clientMsg.Type == "auth_refresh" && clientMsg.Token != "" && c.hub.verifyToken != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				tokenUserID, err := c.hub.verifyToken(ctx, clientMsg.Token)
				cancel()
				if err == nil && tokenUserID != c.userID {
					err = errTokenOfAnotherUser
				}
				if err != nil {
					c.hub.log.Warn("mid-session auth_refresh failed", "user_id", c.userID.String(), "err", err)
					break
				}
				// Verified: token is valid and belongs to this user.
				c.setAuthenticatedAt(time.Now().UTC())
			}
		}
	}
}

func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Hub closed the channel
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			_, _ = w.Write(message)

			// Drain queued messages into the current WebSocket frame
			n := len(c.send)
			for i := 0; i < n; i++ {
				_, _ = w.Write([]byte{'\n'})
				_, _ = w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			// Check user suspension before heartbeat
			if c.hub.isSuspended != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				suspended, err := c.hub.isSuspended(ctx, c.userID, c.getAuthenticatedAt())
				cancel()
				if err == nil && suspended {
					c.hub.log.Info("terminating websocket for suspended user", "user_id", c.userID.String())
					_ = c.conn.WriteControl(
						websocket.CloseMessage,
						websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "user account suspended"),
						time.Now().Add(writeWait),
					)
					return
				}
			}

			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
