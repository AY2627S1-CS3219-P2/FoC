//go:build e2e

// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-10
// Scope: Whole-stack coverage for chat-service through the gateway
//   (/api/chat): the conversation an accepted errand opens, a message sent and
//   read, unread tracking, who may see a conversation, and the conversation
//   going read-only when the courier withdraws. REST only; the WebSocket is not
//   covered.
// Author review: PENDING — <reviewer to complete>

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const chatPath = "/api/chat"

type chatConversation struct {
	ID          string `json:"id"`
	ErrandID    string `json:"errand_id"`
	RequesterID string `json:"requester_id"`
	CourierID   string `json:"courier_id"`
	Status      string `json:"status"`
	ReadOnly    bool   `json:"read_only"`
}

type chatMessage struct {
	ID              string `json:"id"`
	SenderID        string `json:"sender_id"`
	TextContent     string `json:"text_content"`
	ClientMessageID string `json:"client_message_id"`
}

type chatMessagePage struct {
	Messages []chatMessage `json:"messages"`
}

type chatSendRequest struct {
	ClientMessageID string `json:"client_message_id"`
	Text            string `json:"text"`
}

type chatUnread struct {
	HasUnread        bool     `json:"has_unread"`
	UnreadErrandIDs  []string `json:"unread_errand_ids"`
	CourierHasUnread bool     `json:"courier_has_unread"`
}

// conversationFor waits for the conversation chat-service opens when an errand
// is accepted (it follows the errand.accepted event) and returns it as u sees it.
func (s *stack) conversationFor(t *testing.T, u user, errandID string) chatConversation {
	t.Helper()
	var found chatConversation
	eventually(t, eventDeadline, "a conversation for the errand", func(ctx context.Context) (bool, string) {
		r, err := s.request(ctx, http.MethodGet, chatPath+"/conversations?errandId="+errandID, u.token, nil, nil)
		if err != nil {
			return false, err.Error()
		}
		var list []chatConversation
		if r.status != http.StatusOK || r.decode(&list) != nil || len(list) == 0 {
			return false, r.String()
		}
		found = list[0]
		return true, ""
	})
	return found
}

func (s *stack) sendChat(t *testing.T, u user, conversationID, clientID, text string) response {
	t.Helper()
	return s.send(t, http.MethodPost, chatPath+"/conversations/"+conversationID+"/messages", u.token,
		chatSendRequest{ClientMessageID: clientID, Text: text})
}

func (s *stack) chatHistory(t *testing.T, u user, conversationID string) []chatMessage {
	t.Helper()
	r := s.send(t, http.MethodGet, chatPath+"/conversations/"+conversationID+"/messages", u.token, nil)
	expectStatus(t, r, http.StatusOK)
	var page chatMessagePage
	require.NoError(t, r.decode(&page))
	return page.Messages
}

func (s *stack) chatUnread(t *testing.T, u user) chatUnread {
	t.Helper()
	r := s.send(t, http.MethodGet, chatPath+"/unread-summary", u.token, nil)
	expectStatus(t, r, http.StatusOK)
	var summary chatUnread
	require.NoError(t, r.decode(&summary))
	return summary
}

// TestChatFollowsAnAcceptedErrand has the requester and the courier of an
// accepted errand share one conversation: a message reaches the other side once
// even if it is sent twice, the other side sees it as unread until they read
// it, and a stranger and a signed-out caller are refused.
func TestChatFollowsAnAcceptedErrand(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	requester, courier, stranger := s.newStudent(t), s.newStudent(t), s.newStudent(t)
	s.startingBalance(t, requester)
	e := s.createErrand(t, requester, s.errandInput(t, requester, 5)).Errand
	expectStatus(t, s.accept(t, courier, e), http.StatusOK)

	conv := s.conversationFor(t, requester, e.ID)
	assert.Equal(t, requester.id, conv.RequesterID)
	assert.Equal(t, courier.id, conv.CourierID)
	assert.Equal(t, "ACTIVE", conv.Status)
	assert.Equal(t, conv.ID, s.conversationFor(t, courier, e.ID).ID, "both sides share one conversation")

	clientID := randomUUID()
	first := s.sendChat(t, requester, conv.ID, clientID, "Outside the lift, level 2")
	expectStatus(t, first, http.StatusOK)
	var sent chatMessage
	require.NoError(t, first.decode(&sent))
	assert.Equal(t, requester.id, sent.SenderID)

	again := s.sendChat(t, requester, conv.ID, clientID, "Outside the lift, level 2")
	expectStatus(t, again, http.StatusOK)
	var repeated chatMessage
	require.NoError(t, again.decode(&repeated))
	assert.Equal(t, sent.ID, repeated.ID, "the same client message id is one message")

	history := s.chatHistory(t, courier, conv.ID)
	require.Len(t, history, 1)
	assert.Equal(t, "Outside the lift, level 2", history[0].TextContent)

	eventually(t, eventDeadline, "the courier's unread flag", func(ctx context.Context) (bool, string) {
		summary := s.chatUnread(t, courier)
		return summary.HasUnread, "has_unread=false"
	})
	assert.Contains(t, s.chatUnread(t, courier).UnreadErrandIDs, e.ID)
	assert.False(t, s.chatUnread(t, requester).HasUnread, "a sender has nothing unread from their own message")

	expectStatus(t, s.send(t, http.MethodPost, chatPath+"/conversations/"+conv.ID+"/read", courier.token, nil), http.StatusOK)
	assert.False(t, s.chatUnread(t, courier).HasUnread)

	expectStatus(t, s.sendChat(t, requester, conv.ID, randomUUID(), strings.Repeat("x", 251)), http.StatusBadRequest)

	for _, who := range []user{stranger, {}} {
		want := []int{http.StatusForbidden, http.StatusNotFound}
		if who.token == "" {
			want = []int{http.StatusUnauthorized}
		}
		expectStatus(t, s.send(t, http.MethodGet, chatPath+"/conversations/"+conv.ID, who.token, nil), want...)
		expectStatus(t, s.send(t, http.MethodGet, chatPath+"/conversations/"+conv.ID+"/messages", who.token, nil), want...)
		expectStatus(t, s.sendChat(t, who, conv.ID, randomUUID(), "let me in"), want...)
	}
	assert.Len(t, s.chatHistory(t, requester, conv.ID), 1, "a refused caller left no message")
}

// TestChatGoesReadOnlyWhenTheCourierWithdraws expects a courier's withdrawal to
// freeze their conversation: it stays readable, and nobody can add to it.
func TestChatGoesReadOnlyWhenTheCourierWithdraws(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	requester, courier := s.newStudent(t), s.newStudent(t)
	s.startingBalance(t, requester)
	e := s.createErrand(t, requester, s.errandInput(t, requester, 5)).Errand
	expectStatus(t, s.accept(t, courier, e), http.StatusOK)
	conv := s.conversationFor(t, requester, e.ID)
	expectStatus(t, s.sendChat(t, courier, conv.ID, randomUUID(), "On my way"), http.StatusOK)

	detailOf(t, s.act(t, courier, e.ID, "withdraw"), http.StatusOK)

	eventually(t, eventDeadline, "the conversation to freeze", func(ctx context.Context) (bool, string) {
		r, err := s.request(ctx, http.MethodGet, chatPath+"/conversations/"+conv.ID, requester.token, nil, nil)
		if err != nil {
			return false, err.Error()
		}
		var c chatConversation
		if r.status != http.StatusOK || r.decode(&c) != nil {
			return false, r.String()
		}
		return c.Status == "FROZEN" && c.ReadOnly, fmt.Sprintf("status %s, read_only %v", c.Status, c.ReadOnly)
	})
	expectStatus(t, s.sendChat(t, requester, conv.ID, randomUUID(), "still there?"), http.StatusLocked)
	expectStatus(t, s.sendChat(t, courier, conv.ID, randomUUID(), "sorry"), http.StatusLocked, http.StatusForbidden)
	history := s.chatHistory(t, requester, conv.ID)
	require.Len(t, history, 1, "the frozen conversation keeps what was said")
	assert.Equal(t, "On my way", history[0].TextContent)
}
