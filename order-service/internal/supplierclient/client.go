// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-14
// Scope: order.SupplierDirectory over supplier-service's REST API on main
//   (GET /suppliers/{id}), with a bounded timeout and one retry (D-052, D-055).
// Author review: James Teo — reviewed and verified.

// Package supplierclient looks suppliers up in supplier-service over its REST
// API (D-055), so creating and amending an errand can check its supplier
// and copy the supplier's name and location onto it (D-052).
package supplierclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"foc/order-service/internal/order"
	"foc/order-service/internal/shuttle"
)

const (
	// defaultAttemptTimeout bounds one request, so a supplier-service that is
	// slow or down costs an errand create a bounded wait (order-service/AGENTS.md).
	defaultAttemptTimeout = 2 * time.Second
	// maxAttempts is the first try plus one retry. A GET changes nothing, so
	// repeating it is always safe.
	maxAttempts = 2
	// maxBodyBytes caps how much of an answer is read; one supplier is far smaller.
	maxBodyBytes = 1 << 20
)

// Client implements order.SupplierDirectory against supplier-service. It is
// safe for concurrent use.
type Client struct {
	baseURL string
	http    *http.Client
	log     *slog.Logger
	// attemptTimeout is a field, not the constant, only so tests can shorten it.
	attemptTimeout time.Duration
}

// New returns a Client for the supplier-service at baseURL, e.g.
// http://supplier-service:8082. httpClient should carry no overall timeout of
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

// Supplier implements order.SupplierDirectory with GET /suppliers/{id}. It
// returns order.ErrSupplierNotFound on supplier-service's 404, which is also
// its answer for a deleted supplier, and any other error when no usable
// answer came back. A transport error, a timeout or a 5xx is retried once,
// and never past ctx.
func (c *Client) Supplier(ctx context.Context, id uuid.UUID) (order.Supplier, error) {
	for attempt := 1; ; attempt++ {
		sup, err := c.fetch(ctx, id)
		if err == nil {
			return sup, nil
		}
		var again retryable
		if !errors.As(err, &again) || attempt == maxAttempts || ctx.Err() != nil {
			return order.Supplier{}, fmt.Errorf("look up supplier %s: %w", id, err)
		}
		c.log.WarnContext(ctx, "supplier-service lookup failed; retrying once",
			"supplier_id", id, "attempt", attempt, "request_id", order.CorrelationID(ctx), "err", err)
	}
}

// fetch makes one attempt, bounded by attemptTimeout.
func (c *Client) fetch(ctx context.Context, id uuid.UUID) (order.Supplier, error) {
	ctx, cancel := context.WithTimeout(ctx, c.attemptTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/suppliers/"+id.String(), nil)
	if err != nil {
		return order.Supplier{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if correlationID := order.CorrelationID(ctx); correlationID != "" {
		req.Header.Set("X-Request-Id", correlationID)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return order.Supplier{}, retryable{fmt.Errorf("supplier-service did not answer: %w", err)}
	}
	defer resp.Body.Close()
	// Read the whole answer inside this attempt's deadline; a body cut off
	// part way is a transport failure like any other.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return order.Supplier{}, retryable{fmt.Errorf("read supplier-service's answer: %w", err)}
	}

	switch {
	case resp.StatusCode == http.StatusOK:
		return decodeSupplier(body, id)
	case resp.StatusCode == http.StatusNotFound:
		return order.Supplier{}, order.ErrSupplierNotFound
	case resp.StatusCode >= http.StatusInternalServerError:
		return order.Supplier{}, retryable{fmt.Errorf("supplier-service answered %d", resp.StatusCode)}
	default:
		return order.Supplier{}, fmt.Errorf("supplier-service answered unexpected status %d", resp.StatusCode)
	}
}

// supplierResponse is the part of supplier-service's supplierResponse
// (supplier-service/internal/httpapi/dto.go) an errand needs. IsAvailable is
// a pointer so an answer without it is rejected instead of reading as
// "unavailable".
type supplierResponse struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	Building            string `json:"building"`
	Floor               string `json:"floor"`
	LocationDescription string `json:"location_description"`
	IsAvailable         *bool  `json:"is_available"`
	// AI-generated (edited by PENDING): kept on the errand for ranking (D-089).
	// Pointers, so a supplier without a position is told from one at 0, 0.
	Type      string   `json:"type"`
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
}

// decodeSupplier parses a 200 answer. An answer for another supplier is an
// error: copying its name onto the errand would mislabel it (D-052).
func decodeSupplier(body []byte, want uuid.UUID) (order.Supplier, error) {
	var dto supplierResponse
	if err := json.Unmarshal(body, &dto); err != nil {
		return order.Supplier{}, fmt.Errorf("decode supplier-service's answer: %w", err)
	}
	if got, err := uuid.Parse(dto.ID); err != nil || got != want {
		return order.Supplier{}, fmt.Errorf("supplier-service answered with supplier %q", dto.ID)
	}
	if dto.IsAvailable == nil {
		return order.Supplier{}, errors.New("supplier-service's answer has no is_available")
	}
	s := order.Supplier{
		ID:                  want,
		Name:                dto.Name,
		Building:            dto.Building,
		Floor:               dto.Floor,
		LocationDescription: dto.LocationDescription,
		Available:           *dto.IsAvailable,
		Type:                dto.Type,
	}
	// AI-generated (edited by PENDING): a position only when both halves came (D-089).
	if dto.Latitude != nil && dto.Longitude != nil {
		s.Point = &shuttle.Point{Lat: *dto.Latitude, Lon: *dto.Longitude}
	}
	return s, nil
}

// retryable marks a failure after which supplier-service may still answer: a
// transport error, a timeout or a 5xx.
type retryable struct{ err error }

func (r retryable) Error() string { return r.err.Error() }

func (r retryable) Unwrap() error { return r.err }
