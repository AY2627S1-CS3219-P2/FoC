// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: The client for user-service's ranking-model store, forwarding the
//   courier's own access token (D-095, D-099).
// Author review: PENDING — <reviewer to complete>

// Package userclient calls user-service's ranking-model routes,
// /api/v1/users/{uid}/ranking-model, as the courier whose model it is: the
// access token their request to order-service arrived with is forwarded,
// as order-service does for credit-service (D-099). It implements
// ranking.ModelStore.
package userclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"foc/order-service/internal/auth"
	"foc/order-service/internal/ranking"
)

// defaultTimeout bounds one call. It runs inside a courier's GET /open, so it
// is short: a slow user-service costs the recommendation, never the list.
const defaultTimeout = 2 * time.Second

// maxBodyBytes caps what is read of user-service's answer; a model is under
// 10 KB (D-100).
const maxBodyBytes = 64 << 10

// Client is safe for concurrent use.
type Client struct {
	baseURL string
	http    *http.Client
	timeout time.Duration
}

var _ ranking.ModelStore = (*Client)(nil)

// New returns a Client for user-service at baseURL, e.g.
// http://user-service:8081. A nil httpClient gets a fresh one.
func New(baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: httpClient, timeout: defaultTimeout}
}

type modelBody struct {
	Model     json.RawMessage `json:"model"`
	TrainedAt time.Time       `json:"trained_at"`
}

// Load implements ranking.ModelStore: ranking.ErrNoModel on user-service's 404.
func (c *Client) Load(ctx context.Context, courierID uuid.UUID) (ranking.Saved, error) {
	status, body, err := c.do(ctx, http.MethodGet, courierID, nil)
	if err != nil {
		return ranking.Saved{}, err
	}
	switch status {
	case http.StatusOK:
		var b modelBody
		if err := json.Unmarshal(body, &b); err != nil || len(b.Model) == 0 {
			return ranking.Saved{}, fmt.Errorf("user-service sent an unreadable ranking model: %v", err)
		}
		return ranking.Saved{Model: b.Model, TrainedAt: b.TrainedAt.UTC()}, nil
	case http.StatusNotFound:
		return ranking.Saved{}, ranking.ErrNoModel
	default:
		return ranking.Saved{}, fmt.Errorf("user-service answered %d to a ranking-model read", status)
	}
}

// Save implements ranking.ModelStore.
func (c *Client) Save(ctx context.Context, courierID uuid.UUID, s ranking.Saved) error {
	body, err := json.Marshal(modelBody{Model: s.Model, TrainedAt: s.TrainedAt.UTC()})
	if err != nil {
		return fmt.Errorf("encode ranking model: %w", err)
	}
	status, _, err := c.do(ctx, http.MethodPut, courierID, body)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("user-service answered %d to a ranking-model save", status)
	}
	return nil
}

// do makes one call, bounded by the client's timeout, with the caller's token.
func (c *Client) do(ctx context.Context, method string, courierID uuid.UUID, body []byte) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	url := c.baseURL + "/api/v1/users/" + courierID.String() + "/ranking-model"
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token, ok := auth.TokenFromContext(ctx); ok {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("user-service did not answer: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return 0, nil, fmt.Errorf("read user-service's answer: %w", err)
	}
	return resp.StatusCode, data, nil
}
