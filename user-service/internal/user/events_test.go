// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-16
// Scope: Unit tests of the user.registered event's fields and JSON shape.
// Author review: James Teo — reviewed and verified.

package user

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewUserRegisteredEvent(t *testing.T) {
	eventID := uuid.MustParse("6f1c2a5e-0000-4000-8000-000000000001")
	userID := uuid.MustParse("6f1c2a5e-0000-4000-8000-000000000002")
	activatedAt := time.Date(2026, 10, 16, 18, 30, 15, 123000000, time.FixedZone("SGT", 8*60*60))

	event := NewUserRegisteredEvent(eventID, userID, activatedAt)

	if event.EventID != eventID || event.UserID != userID {
		t.Fatalf("event IDs = %s/%s, want %s/%s", event.EventID, event.UserID, eventID, userID)
	}
	if event.EventType != RoutingUserRegistered {
		t.Fatalf("EventType = %q, want %q", event.EventType, RoutingUserRegistered)
	}
	if !event.OccurredAt.Equal(activatedAt) || event.OccurredAt.Location() != time.UTC {
		t.Fatalf("OccurredAt = %s, want %s in UTC", event.OccurredAt, activatedAt)
	}
}

func TestUserRegisteredEventJSON(t *testing.T) {
	eventID := uuid.MustParse("6f1c2a5e-0000-4000-8000-000000000001")
	userID := uuid.MustParse("6f1c2a5e-0000-4000-8000-000000000002")
	activatedAt := time.Date(2026, 10, 16, 18, 30, 15, 0, time.FixedZone("SGT", 8*60*60))

	body, err := json.Marshal(NewUserRegisteredEvent(eventID, userID, activatedAt))
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	want := map[string]any{
		"eventId":    eventID.String(),
		"eventType":  "user.registered",
		"userId":     userID.String(),
		"occurredAt": "2026-10-16T10:30:15Z",
	}
	if len(got) != len(want) {
		t.Fatalf("event JSON = %s, want exactly the fields %v", body, want)
	}
	for field, value := range want {
		if got[field] != value {
			t.Fatalf("event JSON field %q = %v, want %v (body %s)", field, got[field], value, body)
		}
	}
}
