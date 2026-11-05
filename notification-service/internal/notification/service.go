// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-05
// Scope: Who is told about which event (D-141), and listing and marking a
//   user's own notifications (F5.1).
// Author review: PENDING — <reviewer to complete>

package notification

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// The list's paging limits (D-144).
const (
	DefaultLimit = 50
	MaxLimit     = 100
)

// Service holds notification-service's rules.
type Service struct {
	repo          Repository
	confirmWindow time.Duration
}

// NewService returns a Service storing to repo. confirmWindow is C, which the
// delivered notification states (F5.2.3).
func NewService(repo Repository, confirmWindow time.Duration) *Service {
	return &Service{repo: repo, confirmWindow: confirmWindow}
}

// Handle turns one lifecycle event into its notifications. It is safe to call
// again with the same event: nothing is stored twice (F5.5.2). An event type
// this service has no rule for is ignored. An error wrapping ErrInvalidEvent
// will never succeed; any other error may succeed on a retry.
func (s *Service) Handle(ctx context.Context, e Event) error {
	if err := validate(e); err != nil {
		return err
	}
	var (
		change *CourierChange
		out    []Notification
	)
	switch e.Type {
	case EventAccepted:
		change = &CourierChange{ErrandID: e.ErrandID, CourierID: e.CourierID, ChangedAt: e.OccurredAt}
		out = []Notification{newNotification(e, e.RequesterID, "A courier accepted your errand.")}
	case EventPickedUp:
		out = []Notification{newNotification(e, e.RequesterID, "Your errand has been picked up.")}
	case EventDelivered:
		out = []Notification{newNotification(e, e.RequesterID, fmt.Sprintf(
			"Your errand has been delivered. Please confirm it; it completes by itself after %s.",
			formatWindow(s.confirmWindow)))}
	case EventExpired:
		out = []Notification{newNotification(e, e.RequesterID, "Your errand expired without being accepted.")}
	case EventWithdrawn:
		change = &CourierChange{ErrandID: e.ErrandID, ChangedAt: e.OccurredAt}
		out = []Notification{newNotification(e, e.RequesterID, "Your courier withdrew, so your errand is open again.")}
	case EventCompleted:
		out = []Notification{
			newNotification(e, e.RequesterID, "Your errand is complete."),
			newNotification(e, *e.CourierID, creditsMessage(e.Amount)),
		}
	case EventCancelled:
		out = []Notification{newNotification(e, e.RequesterID, "Your errand was cancelled.")}
		// errand.cancelled carries no courier, so the one to tell is the one
		// this service last saw on the errand (D-146).
		courier, err := s.repo.CurrentCourier(ctx, e.ErrandID)
		if err != nil {
			return fmt.Errorf("find the courier of errand %s: %w", e.ErrandID, err)
		}
		if courier != nil {
			out = append(out, newNotification(e, *courier, "An errand assigned to you was cancelled."))
		}
	default:
		return nil
	}
	if err := s.repo.Apply(ctx, change, out); err != nil {
		return fmt.Errorf("store event %s: %w", e.ID, err)
	}
	return nil
}

// validate refuses an event no retry could make usable.
func validate(e Event) error {
	if e.ID == uuid.Nil || e.ErrandID == uuid.Nil || e.RequesterID == uuid.Nil || e.OccurredAt.IsZero() {
		return fmt.Errorf("%w: eventId, errandId, requesterId and occurredAt are required", ErrInvalidEvent)
	}
	switch e.Type {
	case EventAccepted, EventCompleted:
		if e.CourierID == nil || *e.CourierID == uuid.Nil {
			return fmt.Errorf("%w: courierId is required on %s", ErrInvalidEvent, e.Type)
		}
	}
	return nil
}

func newNotification(e Event, recipient uuid.UUID, message string) Notification {
	return Notification{
		ID:          uuid.New(),
		RecipientID: recipient,
		ErrandID:    e.ErrandID,
		EventID:     e.ID,
		StateChange: e.Type,
		Message:     message,
	}
}

// creditsMessage is the courier's half of errand.completed (F5.3.2). It says
// the credits are being transferred, not that they have landed: the event goes
// out before credit-service settles it.
func creditsMessage(amount int) string {
	if amount == 1 {
		return "Errand complete: 1 credit is being transferred to you."
	}
	return fmt.Sprintf("Errand complete: %d credits are being transferred to you.", amount)
}

// formatWindow writes d the way a person would: "24 hours", "90 minutes".
func formatWindow(d time.Duration) string {
	plural := func(n int64, unit string) string {
		if n == 1 {
			return fmt.Sprintf("1 %s", unit)
		}
		return fmt.Sprintf("%d %ss", n, unit)
	}
	switch {
	case d%time.Hour == 0:
		return plural(int64(d/time.Hour), "hour")
	case d%time.Minute == 0:
		return plural(int64(d/time.Minute), "minute")
	default:
		return plural(int64(d.Round(time.Second)/time.Second), "second")
	}
}

// List returns a page of the caller's own notifications, newest first (F5.1.2,
// F5.1.3).
func (s *Service) List(ctx context.Context, caller uuid.UUID, limit, offset int) (Page, error) {
	if limit < 1 || limit > MaxLimit {
		return Page{}, fmt.Errorf("%w: limit must be from 1 to %d", ErrInvalidPage, MaxLimit)
	}
	if offset < 0 {
		return Page{}, fmt.Errorf("%w: offset must not be negative", ErrInvalidPage)
	}
	page, err := s.repo.List(ctx, caller, limit, offset)
	if err != nil {
		return Page{}, fmt.Errorf("list notifications: %w", err)
	}
	return page, nil
}

// MarkRead marks one of the caller's own notifications as read (F5.1.5).
func (s *Service) MarkRead(ctx context.Context, caller, id uuid.UUID) error {
	if err := s.repo.MarkRead(ctx, caller, id); err != nil {
		return fmt.Errorf("mark notification %s read: %w", id, err)
	}
	return nil
}
