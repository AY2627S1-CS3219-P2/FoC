// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: Posting an errand: validation (F3.1.2-F3.1.4), supplier check (D-052),
//   duplicate-submission replay and reserve-before-save with a queued
//   refund (D-047).
// Author review: James Teo — reviewed and verified.

package order

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// AI-generated (edited by James Teo).
// MaxOpenErrands is how many errands OPEN before their deadlines stop a
// requester posting another (D-058). Only create checks it: a withdraw can
// still return an errand to OPEN past it.
const MaxOpenErrands = 5

// CreateInput is a new errand as the requester submitted it (F3.1.1).
type CreateInput struct {
	SupplierID       uuid.UUID
	ItemDescription  string
	DeliveryLocation string
	DeliveryNote     string
	Reward           int
	// ScheduledAt is nil for an ordinary errand.
	ScheduledAt *time.Time
	// RequestKey is the request's X-Request-Id; empty when it had none (D-047).
	RequestKey    string
	CorrelationID string
}

// Create posts an errand in OPEN or SCHEDULED (F3.1.7). replayed is true when
// the requester already created an errand with in.RequestKey: that errand is
// returned and no credits are touched (D-047).
func (s *Service) Create(ctx context.Context, c Caller, in CreateInput) (o Order, replayed bool, err error) {
	if c.Role != RoleStudent {
		return Order{}, false, forbidden("only students can post errands")
	}
	in.ItemDescription = strings.TrimSpace(in.ItemDescription)
	in.DeliveryNote = strings.TrimSpace(in.DeliveryNote)
	if err := validateCreate(in); err != nil {
		return Order{}, false, err
	}

	if in.RequestKey != "" {
		// D-047: a twin with the same key waits here until this create has
		// returned, then finds its errand and replays it without touching
		// credit. The lock is held until repo.Create below has returned.
		unlock, err := s.createKeys.lock(ctx, c.UserID.String()+"\x00"+in.RequestKey)
		if err != nil {
			return Order{}, false, unavailable("could not post the errand; please try again", err)
		}
		defer unlock()
		// AI-generated (edited by James Teo).
		existing, found, err := s.findReplay(ctx, c.UserID, in.RequestKey)
		if err != nil {
			return Order{}, false, err
		}
		if found {
			return existing, true, nil
		}
	}

	// D-058: a requester at the limit is told so here, before credit is
	// touched. Two creates can both pass this count; the one repo.Create makes
	// under the requester's lock is what stops the second.
	if in.ScheduledAt == nil {
		open, err := s.repo.CountOpenByRequester(ctx, c.UserID, s.clock.Now())
		if err != nil {
			return Order{}, false, unavailable("could not post the errand; please try again", err)
		}
		if open >= MaxOpenErrands {
			// AI-generated (edited by James Teo).
			// The errand that filled the last place may be this submission's own,
			// saved after the lookup above by a twin on another replica, where the
			// per-key lock does not reach. That twin is replayed, not refused
			// (D-047).
			if in.RequestKey != "" {
				existing, found, err := s.findReplay(ctx, c.UserID, in.RequestKey)
				if err != nil {
					return Order{}, false, err
				}
				if found {
					return existing, true, nil
				}
			}
			return Order{}, false, tooManyOpen()
		}
	}

	// Validate against the time after supplier-service has answered so a
	// schedule cannot pass validation and become due during that call.
	snapshot, err := s.supplierSnapshot(ctx, in.SupplierID)
	if err != nil {
		return Order{}, false, err
	}
	now := s.clock.Now()
	if err := validateScheduledTime(in.ScheduledAt, now); err != nil {
		return Order{}, false, err
	}
	state := StateOpen
	var expiresAt *time.Time
	scheduledAt := in.ScheduledAt
	if in.ScheduledAt == nil {
		expiresAt = timePtr(now.Add(s.cfg.ExpiryPeriod))
	} else {
		state = StateScheduled
		scheduledAt = timePtr(in.ScheduledAt.UTC())
	}
	o = Order{
		ID:               uuid.New(),
		RequesterID:      c.UserID,
		Supplier:         snapshot,
		ItemDescription:  in.ItemDescription,
		DeliveryLocation: in.DeliveryLocation,
		DeliveryNote:     in.DeliveryNote,
		Reward:           in.Reward,
		State:            state,
		ScheduledAt:      scheduledAt,
		Version:          1,
		RequestKey:       in.RequestKey,
		CreatedAt:        now,
		UpdatedAt:        now,
		StateChangedAt:   now,
		ExpiresAt:        expiresAt,
	}

	// D-047: queue the refund first, so every way this request can end leaves
	// exactly one of {errand saved, refund sent}. Saving the errand deletes it.
	guard := newEvent(uuid.New(), RoutingFailed, o, nil, now, in.CorrelationID)
	guard.AvailableAt = now.Add(s.cfg.RefundDelay)
	if err := s.repo.QueueEvent(ctx, *guard); err != nil {
		return Order{}, false, unavailable("could not post the errand; please try again", err)
	}

	err = s.credit.Reserve(ctx, Reservation{
		ErrandID:      o.ID,
		RequesterID:   o.RequesterID,
		Amount:        o.Reward,
		CorrelationID: in.CorrelationID,
	})
	if errors.Is(err, ErrInsufficientCredits) {
		// The queued refund still fires; credit acks it as a no-op (pending Seth, D-047).
		return Order{}, false, insufficientCredits(err)
	}
	if errors.Is(err, ErrCreditRejected) {
		// A definite refusal (e.g. 401 while credit-service does not yet read
		// a forwarded token, D-056) is a misconfiguration retrying cannot fix.
		s.log.ErrorContext(ctx, "credit-service refused a reservation; queued refund will release any hold",
			"errand_id", o.ID, "request_id", in.CorrelationID, "err", err)
		return Order{}, false, unavailable("credits are unavailable right now; please try again", err)
	}
	if err != nil {
		s.log.WarnContext(ctx, "credit reservation failed or outcome unknown; queued refund will release any hold",
			"errand_id", o.ID, "request_id", in.CorrelationID, "err", err)
		return Order{}, false, unavailable("credits are unavailable right now; please try again", err)
	}

	history := HistoryEntry{
		OrderID:       o.ID,
		Action:        ActionCreate,
		ToState:       state,
		ActorID:       uuidPtr(c.UserID),
		ActorKind:     ActorRequester,
		OccurredAt:    now,
		CorrelationID: in.CorrelationID,
	}
	err = s.repo.Create(ctx, o, history, guard.EventID)
	switch {
	case err == nil:
		return o, false, nil
	case errors.Is(err, ErrDuplicateRequestKey):
		// A twin with the same X-Request-Id won on another replica, where the
		// lock above does not reach; this attempt's reservation is released
		// by its queued refund.
		existing, ferr := s.repo.FindByRequestKey(ctx, c.UserID, in.RequestKey)
		if ferr != nil {
			return Order{}, false, unavailable("could not post the errand; please try again", ferr)
		}
		return existing, true, nil
	case errors.Is(err, ErrGuardGone):
		return Order{}, false, unavailable("posting took too long and was undone; your credits will be returned", err)
	// AI-generated (edited by James Teo).
	case errors.Is(err, ErrTooManyOpen):
		// A concurrent create took the last place after the count above; this
		// attempt's reservation is released by its queued refund (D-047).
		return Order{}, false, tooManyOpenAfterReserve()
	default:
		return Order{}, false, unavailable("could not post the errand; please try again", err)
	}
}

// AI Assistance Disclosure: Codex (GPT-6), 2026-10-28 — enforces the owner's
// recorded future-time and previous-calendar-day scheduling boundary.
func validateScheduledTime(scheduledAt *time.Time, now time.Time) error {
	if scheduledAt == nil {
		return nil
	}
	scheduledUTC := scheduledAt.UTC()
	if !scheduledUTC.After(now.UTC()) {
		return invalid("scheduled time must be in the future")
	}
	earliest := time.Date(scheduledUTC.Year(), scheduledUTC.Month(), scheduledUTC.Day()-1, 0, 0, 0, 0, time.UTC)
	if now.UTC().Before(earliest) {
		return invalid("scheduled time can be set starting at 00:00 UTC on the previous calendar day")
	}
	return nil
}

func validateCreate(in CreateInput) error {
	if in.SupplierID == uuid.Nil {
		return invalid("choose a supplier")
	}
	if err := validateItem(in.ItemDescription); err != nil {
		return err
	}
	if err := validateLocation(in.DeliveryLocation); err != nil {
		return err
	}
	if err := validateNote(in.DeliveryNote); err != nil {
		return err
	}
	return validateReward(in.Reward)
}

// AI-generated (edited by James Teo).
// tooManyOpen is D-058's refusal of a create past MaxOpenErrands.
func tooManyOpen() error {
	return conflict(fmt.Sprintf(
		"you already have %d open errands; cancel one or wait for one to be accepted before posting another", MaxOpenErrands))
}

// AI-generated (edited by James Teo).
// tooManyOpenAfterReserve is the same refusal once credit was reserved for the
// errand, which its queued refund returns (D-047).
func tooManyOpenAfterReserve() error {
	return conflict(fmt.Sprintf(
		"you already have %d open errands, so this errand was not posted; your credits will be returned", MaxOpenErrands))
}

// AI-generated (edited by James Teo).
// findReplay looks up the errand requesterID already created with key. found
// is false when there is none; err is the answer to give when the lookup fails.
func (s *Service) findReplay(ctx context.Context, requesterID uuid.UUID, key string) (o Order, found bool, err error) {
	o, err = s.repo.FindByRequestKey(ctx, requesterID, key)
	switch {
	case err == nil:
		return o, true, nil
	case errors.Is(err, ErrNotFound):
		return Order{}, false, nil
	default:
		return Order{}, false, unavailable("could not post the errand; please try again", err)
	}
}

// AI-generated (edited by James Teo).
// validateItem and validateNote are the one text rule create and amend share
// (F3.7.2). Neither may hold a control character or a line break, except that
// the note may keep newlines ("\n"); that also keeps out NUL, the one character
// a PostgreSQL text column cannot store. Neither may hold a text-direction
// control, which can make what a courier reads differ from what is stored. An
// item must show at least one visible character: format characters such as a
// zero-width space, and blank fillers, do not count.
func validateItem(item string) error {
	if !strings.ContainsFunc(item, isVisible) {
		return invalid("describe the item to pick up")
	}
	if strings.ContainsFunc(item, isLineBreakOrControl) {
		return invalid("the item description cannot contain line breaks or control characters")
	}
	if strings.ContainsFunc(item, isBidiControl) {
		return invalid("the item description cannot contain text-direction control characters")
	}
	if charLen(item) > MaxItemDescription {
		return invalid("the item description can be at most 280 characters")
	}
	return nil
}

func validateLocation(loc string) error {
	if !isDeliveryLocation(loc) {
		return invalid("choose a delivery location from the campus list")
	}
	return nil
}

// AI-generated (edited by James Teo).
func validateNote(note string) error {
	if strings.ContainsFunc(note, isNoteControl) {
		return invalid("the note can contain newlines but no other line breaks or control characters")
	}
	if strings.ContainsFunc(note, isBidiControl) {
		return invalid("the note cannot contain text-direction control characters")
	}
	if charLen(note) > MaxDeliveryNote {
		return invalid("the note can be at most 140 characters")
	}
	return nil
}

// AI-generated (edited by James Teo).
// isVisible reports whether r shows on screen: graphic, not a space and not a
// blank filler. A format character (Unicode category Cf) is not graphic.
func isVisible(r rune) bool {
	return unicode.IsGraphic(r) && !unicode.IsSpace(r) && !isBlankFiller(r)
}

// AI-generated (edited by James Teo).
// isBlankFiller reports whether r is a letter or symbol that draws nothing,
// though Go counts it as graphic: the Hangul fillers (U+115F, U+1160, U+3164,
// U+FFA0) and the blank Braille pattern (U+2800).
func isBlankFiller(r rune) bool {
	switch r {
	case '\u115F', '\u1160', '\u3164', '\uFFA0', '\u2800':
		return true
	}
	return false
}

// AI-generated (edited by James Teo).
// isLineBreakOrControl reports whether r is a C0 or C1 control character, or
// one of the two line breaks that are not: U+2028 LINE SEPARATOR and U+2029
// PARAGRAPH SEPARATOR.
func isLineBreakOrControl(r rune) bool {
	return unicode.IsControl(r) || r == '\u2028' || r == '\u2029'
}

// AI-generated (edited by James Teo).
// isNoteControl is isLineBreakOrControl without the newline a note may keep.
func isNoteControl(r rune) bool { return r != '\n' && isLineBreakOrControl(r) }

// AI-generated (edited by James Teo).
// isBidiControl reports whether r is a text-direction embedding or override
// (U+202A to U+202E) or isolate (U+2066 to U+2069).
func isBidiControl(r rune) bool {
	return ('\u202A' <= r && r <= '\u202E') || ('\u2066' <= r && r <= '\u2069')
}

// validateReward enforces F3.1.2: a positive whole number of credits.
func validateReward(reward int) error {
	if reward < 1 {
		return invalid("the reward must be at least 1 credit")
	}
	if reward > MaxReward {
		return invalid(fmt.Sprintf("the reward can be at most %d credits", MaxReward))
	}
	return nil
}

// keyLocks serializes the creates that share a key within this process, so a
// twin waits for the first to finish instead of reserving credit beside it
// (D-047). With more than one replica, the unique request key, the replay
// after ErrDuplicateRequestKey and D-047's queued refund still cover a twin
// that lands on another one.
type keyLocks struct {
	mu      sync.Mutex
	entries map[string]*keyLock
}

// keyLock is one key's lock. An entry is removed once nobody holds or waits
// on it, so the map holds only the keys in use.
type keyLock struct {
	held chan struct{} // 1-buffered: full while a create holds the key
	refs int           // creates holding or waiting on the key; guarded by keyLocks.mu
}

func newKeyLocks() *keyLocks {
	return &keyLocks{entries: map[string]*keyLock{}}
}

// lock waits until key is free, then holds it. It gives up with ctx's error if
// ctx is done first. On success the caller must call unlock exactly once.
func (l *keyLocks) lock(ctx context.Context, key string) (unlock func(), err error) {
	l.mu.Lock()
	e, ok := l.entries[key]
	if !ok {
		e = &keyLock{held: make(chan struct{}, 1)}
		l.entries[key] = e
	}
	e.refs++
	l.mu.Unlock()

	select {
	case e.held <- struct{}{}:
		return func() {
			<-e.held
			l.release(key, e)
		}, nil
	case <-ctx.Done():
		l.release(key, e)
		return nil, fmt.Errorf("wait for an earlier create with the same request key: %w", ctx.Err())
	}
}

// release drops one reference to key's entry, and removes the entry once
// nobody holds or waits on it.
func (l *keyLocks) release(key string, e *keyLock) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e.refs--
	if e.refs == 0 {
		delete(l.entries, key)
	}
}
