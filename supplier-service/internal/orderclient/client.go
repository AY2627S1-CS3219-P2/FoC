// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-16
// Scope: supplier.ErrandCounter over order-service's REST API
//   (GET /suppliers/{supplierId}/active-errands), forwarding the admin's token.
// Author review: James Teo — reviewed and verified.

// Package orderclient asks order-service how many errands that are not
// finished yet reference a supplier, so supplier-service can refuse to delete
// one that is still in use (FR F2.2.3).
package orderclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// requestTimeout bounds one call, so an order-service that is slow or down
	// fails the delete after a bounded wait instead of hanging the admin.
	requestTimeout = 3 * time.Second
	// maxBodyBytes caps how much of an answer is read; a count is a few bytes.
	maxBodyBytes = 1 << 16
)

// tokenKey is the context key under which WithToken stores the Authorization
// value.
type tokenKey struct{}

// WithToken returns a copy of ctx carrying authorization, the incoming
// request's Authorization header value ("Bearer <access token>"), for
// ActiveErrands to forward. order-service checks itself that the caller is an
// ADMIN, so it needs the admin's own token.
func WithToken(ctx context.Context, authorization string) context.Context {
	return context.WithValue(ctx, tokenKey{}, authorization)
}

// tokenFrom returns the Authorization value WithToken put on ctx, or "".
func tokenFrom(ctx context.Context) string {
	authorization, _ := ctx.Value(tokenKey{}).(string)
	return authorization
}

// Client implements supplier.ErrandCounter against order-service. It is safe
// for concurrent use.
type Client struct {
	baseURL string
	http    *http.Client
	// timeout is a field, not the constant, only so tests can shorten it.
	timeout time.Duration
}

// New returns a Client for the order-service at baseURL, e.g.
// http://order-service:8083.
func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{},
		timeout: requestTimeout,
	}
}

// activeErrandsResponse is the part of order-service's answer this check
// needs. ActiveErrands is a pointer so an answer without it is rejected
// instead of reading as zero, which would let the delete through.
type activeErrandsResponse struct {
	ActiveErrands *int `json:"activeErrands"`
}

// ActiveErrands implements supplier.ErrandCounter with
// GET /suppliers/{supplierId}/active-errands, sending the Authorization value
// WithToken put on ctx. Anything but a well-formed 200 is an error, so the
// caller never mistakes "could not ask" for "none".
func (c *Client) ActiveErrands(ctx context.Context, supplierID string) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	endpoint := c.baseURL + "/suppliers/" + url.PathEscape(supplierID) + "/active-errands"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if authorization := tokenFrom(ctx); authorization != "" {
		req.Header.Set("Authorization", authorization)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("order-service did not answer: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("order-service answered status %d", resp.StatusCode)
	}

	var body activeErrandsResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(&body); err != nil {
		return 0, fmt.Errorf("decode order-service's answer: %w", err)
	}
	if body.ActiveErrands == nil || *body.ActiveErrands < 0 {
		return 0, errors.New("order-service's answer has no valid activeErrands")
	}
	return *body.ActiveErrands, nil
}
