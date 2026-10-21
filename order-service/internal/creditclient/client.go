// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: order.CreditClient over credit-service's REST API (its api/openapi.yaml):
//   reserve with one retry on the same errandId, adjust with none, each
//   forwarding the requester's access token (D-047, D-055, D-056, D-057).
// Author review: James Teo — reviewed and verified.

// Package creditclient calls credit-service's REST API (D-055) for the two
// synchronous credit operations order-service makes: reserving an errand's
// reward before the errand is saved (D-047), and setting the held amount when
// the reward is amended (D-057). Settlement is by events (D-048). Every call
// forwards the access token the request arrived with (D-056): only the
// errand's requester reserves or amends, so it is always the requester's.
package creditclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"foc/order-service/internal/auth"
	"foc/order-service/internal/order"
)

const (
	// defaultAttemptTimeout bounds one request. Adjust runs while the errand's
	// row is locked (D-057), so an unbounded wait would hold that lock too.
	defaultAttemptTimeout = 2 * time.Second
	// reserveAttempts is the first try plus one retry.
	reserveAttempts = 2
	// reservationsPath is credit-service's reservation collection.
	reservationsPath = "/api/v1/credits/reservations"
	// maxDrainBytes caps how much of an answer is read and thrown away so the
	// connection can be reused; the status code is the whole answer.
	maxDrainBytes = 64 << 10
)

// Client implements order.CreditClient against credit-service. It is safe for
// concurrent use.
type Client struct {
	baseURL string
	http    *http.Client
	log     *slog.Logger
	// attemptTimeout is a field, not the constant, only so tests can shorten it.
	attemptTimeout time.Duration
}

// New returns a Client for the credit-service at baseURL, e.g.
// http://credit-service:8084. httpClient should carry no overall timeout of
// its own: each attempt is bounded here. A nil httpClient gets a fresh
// default one and a nil log discards.
func New(baseURL string, httpClient *http.Client, log *slog.Logger) *Client {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Client{
		baseURL:        strings.TrimRight(baseURL, "/"),
		http:           httpClient,
		log:            log,
		attemptTimeout: defaultAttemptTimeout,
	}
}

// reserveRequest is credit-service's CreateReservationRequest.
type reserveRequest struct {
	ErrandID uuid.UUID `json:"errandId"`
	Amount   int       `json:"amount"`
}

// adjustRequest is credit-service's AdjustRewardRequest.
type adjustRequest struct {
	NewReward int `json:"newReward"`
}

// Reserve implements order.CreditClient with POST
// /api/v1/credits/reservations. It returns nil once the credits are held,
// order.ErrInsufficientCredits on 422, an error wrapping
// order.ErrCreditRejected on 400, 401, 403 or 404, and any other error when
// the outcome is not known. A transport error, a timeout or a 5xx is retried
// once with the same errandId, and never past ctx.
//
// AI-generated (edited by James Teo).
// A 422 on the retry is an unknown outcome, not insufficient credits: the
// first attempt may have made the hold that leaves too little for the retry.
//
// A 200 means held too: it is credit-service's answer to a request identical
// to a hold it already made, such as a retry after a lost answer. A 409 is an
// unknown outcome on either attempt (D-073).
func (c *Client) Reserve(ctx context.Context, r order.Reservation) error {
	body, err := json.Marshal(reserveRequest{ErrandID: r.ErrandID, Amount: r.Amount})
	if err != nil {
		return fmt.Errorf("reserve credits for errand %s: encode request: %w", r.ErrandID, err)
	}
	for attempt := 1; ; attempt++ {
		status, err := c.send(ctx, http.MethodPost, c.baseURL+reservationsPath, body, r.CorrelationID)
		// A retry needs an attempt left and a caller still waiting for it.
		canRetry := attempt < reserveAttempts && ctx.Err() == nil
		switch {
		case err != nil && canRetry:
			c.log.WarnContext(ctx, "credit reservation got no answer; retrying once with the same errandId",
				"errand_id", r.ErrandID, "request_id", r.CorrelationID, "attempt", attempt, "err", err)
			continue
		case err != nil:
			return fmt.Errorf("reserve credits for errand %s: %w", r.ErrandID, err)
		// A retry that finds the first attempt's hold gets 200, not 409 (D-073).
		case status == http.StatusCreated, status == http.StatusOK:
			return nil
		case status == http.StatusConflict:
			// credit-service holds a reservation for this errandId that doesn't
			// match this request: an identical one would have answered 200. So
			// whose hold it is is unknown on either attempt, and the create's
			// queued refund for this errandId settles it (D-047).
			return fmt.Errorf("reserve credits for errand %s: credit-service holds a reservation for it that doesn't match this request; outcome unknown", r.ErrandID)
		// AI-generated (edited by James Teo).
		case status == http.StatusUnprocessableEntity && attempt > 1:
			// credit-service may check the balance before it sees the first
			// attempt's hold for this errandId, and that hold may be what
			// leaves too little. The create's queued refund releases it (D-047).
			c.log.WarnContext(ctx, "credit reservation retry answered 422; the first attempt may have reserved, so the outcome is unknown",
				"errand_id", r.ErrandID, "request_id", r.CorrelationID)
			return fmt.Errorf("reserve credits for errand %s: the retry answered %d after an attempt with no answer; outcome unknown", r.ErrandID, status)
		case status == http.StatusUnprocessableEntity:
			return fmt.Errorf("reserve credits for errand %s: %w", r.ErrandID, order.ErrInsufficientCredits)
		case status == http.StatusBadRequest, status == http.StatusUnauthorized,
			status == http.StatusForbidden, status == http.StatusNotFound:
			return fmt.Errorf("reserve credits for errand %s: credit-service answered %d: %w",
				r.ErrandID, status, order.ErrCreditRejected)
		case status >= http.StatusInternalServerError && canRetry:
			c.log.WarnContext(ctx, "credit reservation failed on credit-service; retrying once with the same errandId",
				"errand_id", r.ErrandID, "request_id", r.CorrelationID, "attempt", attempt, "status", status)
			continue
		default:
			return fmt.Errorf("reserve credits for errand %s: credit-service answered %d", r.ErrandID, status)
		}
	}
}

// Adjust implements order.CreditClient with PATCH
// /api/v1/credits/reservations/{errandId}. It returns nil when applied,
// order.ErrInsufficientCredits on 422, an error wrapping
// order.ErrCreditRejected on 400, 401, 403, 404 or 409, and any other error
// when the outcome is not known. It is never retried here: when the outcome
// is unknown, the order service queues errand.reward_set for the errand as
// saved instead (D-057).
func (c *Client) Adjust(ctx context.Context, errandID, _ uuid.UUID, newReward int, correlationID string) error {
	body, err := json.Marshal(adjustRequest{NewReward: newReward})
	if err != nil {
		return fmt.Errorf("adjust credits for errand %s: encode request: %w", errandID, err)
	}
	status, err := c.send(ctx, http.MethodPatch, c.baseURL+reservationsPath+"/"+errandID.String(), body, correlationID)
	if err != nil {
		return fmt.Errorf("adjust credits for errand %s: %w", errandID, err)
	}
	switch status {
	case http.StatusOK:
		return nil
	case http.StatusUnprocessableEntity:
		return fmt.Errorf("adjust credits for errand %s: %w", errandID, order.ErrInsufficientCredits)
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict:
		return fmt.Errorf("adjust credits for errand %s: credit-service answered %d: %w", errandID, status, order.ErrCreditRejected)
	default:
		return fmt.Errorf("adjust credits for errand %s: credit-service answered %d", errandID, status)
	}
}

// send makes one attempt, bounded by attemptTimeout, and returns the status
// credit-service answered, or an error when no answer came back.
func (c *Client) send(ctx context.Context, method, url string, body []byte, correlationID string) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, c.attemptTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if token, ok := auth.TokenFromContext(ctx); ok {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if correlationID != "" {
		req.Header.Set("X-Request-Id", correlationID)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("credit-service did not answer: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxDrainBytes))
	return resp.StatusCode, nil
}
