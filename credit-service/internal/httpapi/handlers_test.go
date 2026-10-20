// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Added comprehensive unit tests for HTTP API handlers (reads, reservations, adjustments, health) per sourceOfTruth.md Phase 9.
// Author review: PENDING - reviewer to complete

package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"foc/credit-service/internal/auth"
	"foc/credit-service/internal/credit"
	"foc/credit-service/internal/gen"
	"foc/credit-service/internal/httpapi"

	"github.com/google/uuid"
)

type mockReadRepo struct {
	getAccountFn       func(ctx context.Context, userID uuid.UUID) (*credit.Account, error)
	listTransactionsFn func(ctx context.Context, userID uuid.UUID, limit int, cursor *string) (*credit.PaginatedTransactions, error)
}

func (m *mockReadRepo) GetAccount(ctx context.Context, userID uuid.UUID) (*credit.Account, error) {
	if m.getAccountFn != nil {
		return m.getAccountFn(ctx, userID)
	}
	return nil, credit.ErrWalletNotReady
}

func (m *mockReadRepo) ListTransactions(ctx context.Context, userID uuid.UUID, limit int, cursor *string) (*credit.PaginatedTransactions, error) {
	if m.listTransactionsFn != nil {
		return m.listTransactionsFn(ctx, userID, limit, cursor)
	}
	return &credit.PaginatedTransactions{}, nil
}

type mockReservationRepo struct {
	createReservationFn func(ctx context.Context, errandID, requesterID uuid.UUID, amount int64) (*credit.Reservation, bool, error)
	adjustRewardFn      func(ctx context.Context, errandID uuid.UUID, requesterID uuid.UUID, newReward int64, operationID uuid.UUID) (*credit.OperationResult, *credit.Reservation, error)
}

func (m *mockReservationRepo) CreateReservation(ctx context.Context, errandID, requesterID uuid.UUID, amount int64) (*credit.Reservation, bool, error) {
	if m.createReservationFn != nil {
		return m.createReservationFn(ctx, errandID, requesterID, amount)
	}
	return nil, false, errors.New("not implemented")
}

func (m *mockReservationRepo) AdjustReservationReward(ctx context.Context, errandID uuid.UUID, requesterID uuid.UUID, newReward int64, operationID uuid.UUID) (*credit.OperationResult, *credit.Reservation, error) {
	if m.adjustRewardFn != nil {
		return m.adjustRewardFn(ctx, errandID, requesterID, newReward, operationID)
	}
	return nil, nil, errors.New("not implemented")
}

func (m *mockReservationRepo) ApplyRewardSet(ctx context.Context, errandID, eventID uuid.UUID, newReward int64, version int64, occurredAt time.Time) error {
	return nil
}

func setupTestRouter(t *testing.T, readRepo credit.ReadRepository, resRepo credit.ReservationRepository, healthErr error) (http.Handler, *auth.Verifier, *issuer) {
	t.Helper()
	iss := newIssuer(t)
	jwks := httptest.NewServer(iss.jwksHandler())
	t.Cleanup(jwks.Close)

	verifier := auth.NewVerifier(jwks.URL, nil)

	var opts []func(*credit.Service)
	if readRepo != nil {
		opts = append(opts, credit.WithReadRepository(readRepo))
	}
	if resRepo != nil {
		opts = append(opts, credit.WithReservationRepository(resRepo))
	}
	svc := credit.NewService(nil, opts...)

	healthCheck := func(ctx context.Context) error {
		return healthErr
	}

	router := httpapi.NewRouter(healthCheck, verifier, nil, svc, nil)
	return router, verifier, iss
}

func TestHealthEndpoints(t *testing.T) {
	t.Run("liveness returns 200", func(t *testing.T) {
		router, _, _ := setupTestRouter(t, nil, nil, nil)
		req := httptest.NewRequest(http.MethodGet, "/health/live", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var resp gen.HealthResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp.Status != gen.Ok {
			t.Fatalf("status = %v, want ok", resp.Status)
		}
	})

	t.Run("readiness returns 200 when healthy", func(t *testing.T) {
		router, _, _ := setupTestRouter(t, nil, nil, nil)
		req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	})

	t.Run("readiness returns 503 when unhealthy", func(t *testing.T) {
		router, _, _ := setupTestRouter(t, nil, nil, errors.New("db down"))
		req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", rec.Code)
		}
		var errResp gen.ErrorResponse
		if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if errResp.Code != gen.SERVICEUNAVAILABLE {
			t.Fatalf("code = %v, want SERVICE_UNAVAILABLE", errResp.Code)
		}
	})
}

func TestGetMyCreditAccount(t *testing.T) {
	testUserID := uuid.New()

	t.Run("rejects request without bearer token", func(t *testing.T) {
		router, _, _ := setupTestRouter(t, nil, nil, nil)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/credits/accounts/me", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("returns 200 with projections for consistent account", func(t *testing.T) {
		repo := &mockReadRepo{
			getAccountFn: func(ctx context.Context, userID uuid.UUID) (*credit.Account, error) {
				return &credit.Account{
					UserID:           userID,
					AvailableCredits: 80,
					ReservedCredits:  20,
					IntegrityStatus:  credit.IntegrityConsistent,
					UpdatedAt:        time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC),
				}, nil
			},
		}
		router, _, iss := setupTestRouter(t, repo, nil, nil)
		token := iss.mint(t, tokenOpts{subject: testUserID.String()})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/credits/accounts/me", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
		}
		var resp gen.AccountResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp.CreditStatus != gen.READY {
			t.Errorf("CreditStatus = %v, want READY", resp.CreditStatus)
		}
		if resp.AvailableCredits == nil || *resp.AvailableCredits != 80 {
			t.Errorf("AvailableCredits = %v, want 80", resp.AvailableCredits)
		}
		if resp.ReservedCredits == nil || *resp.ReservedCredits != 20 {
			t.Errorf("ReservedCredits = %v, want 20", resp.ReservedCredits)
		}
		if resp.UserId != testUserID {
			t.Errorf("UserId = %v, want %v", resp.UserId, testUserID)
		}
	})

	t.Run("returns 200 and omits projections for flagged account", func(t *testing.T) {
		repo := &mockReadRepo{
			getAccountFn: func(ctx context.Context, userID uuid.UUID) (*credit.Account, error) {
				return &credit.Account{
					UserID:           userID,
					AvailableCredits: 80,
					ReservedCredits:  20,
					IntegrityStatus:  credit.IntegrityFlagged,
					UpdatedAt:        time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC),
				}, nil
			},
		}
		router, _, iss := setupTestRouter(t, repo, nil, nil)
		token := iss.mint(t, tokenOpts{subject: testUserID.String()})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/credits/accounts/me", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
		}
		var resp gen.AccountResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp.CreditStatus != gen.TEMPORARILYUNAVAILABLE {
			t.Errorf("CreditStatus = %v, want TEMPORARILY_UNAVAILABLE", resp.CreditStatus)
		}
		if resp.AvailableCredits != nil {
			t.Errorf("AvailableCredits should be omitted, got %v", *resp.AvailableCredits)
		}
		if resp.ReservedCredits != nil {
			t.Errorf("ReservedCredits should be omitted, got %v", *resp.ReservedCredits)
		}
	})

	t.Run("returns 404 WALLET_NOT_READY when wallet does not exist", func(t *testing.T) {
		repo := &mockReadRepo{
			getAccountFn: func(ctx context.Context, userID uuid.UUID) (*credit.Account, error) {
				return nil, credit.ErrWalletNotReady
			},
		}
		router, _, iss := setupTestRouter(t, repo, nil, nil)
		token := iss.mint(t, tokenOpts{subject: testUserID.String()})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/credits/accounts/me", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
		var errResp gen.ErrorResponse
		if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if errResp.Code != gen.WALLETNOTREADY {
			t.Fatalf("code = %v, want WALLET_NOT_READY", errResp.Code)
		}
	})
}

func TestListMyCreditTransactions(t *testing.T) {
	testUserID := uuid.New()

	t.Run("rejects invalid limit bound", func(t *testing.T) {
		router, _, iss := setupTestRouter(t, &mockReadRepo{}, nil, nil)
		token := iss.mint(t, tokenOpts{subject: testUserID.String()})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/credits/accounts/me/transactions?limit=150", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("returns grouped operations and next cursor", func(t *testing.T) {
		opID := uuid.New()
		errandID := uuid.New()
		nextCursor := "eyJ2IjoxLCJjcmVhdGVkQXQiOiIyMDI2LTEwLTE5VDEyOjAwOjAwWiIsIm9wZXJhdGlvbklkIjoiYWJjIn0"

		repo := &mockReadRepo{
			listTransactionsFn: func(ctx context.Context, userID uuid.UUID, limit int, cursor *string) (*credit.PaginatedTransactions, error) {
				return &credit.PaginatedTransactions{
					Items: []credit.TransactionOperation{
						{
							OperationID:    opID,
							ErrandID:       &errandID,
							Reason:         credit.TransactionReasonErrandReservation,
							AvailableDelta: -25,
							ReservedDelta:  25,
							CreatedAt:      time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC),
						},
					},
					NextCursor: &nextCursor,
				}, nil
			},
		}

		router, _, iss := setupTestRouter(t, repo, nil, nil)
		token := iss.mint(t, tokenOpts{subject: testUserID.String()})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/credits/accounts/me/transactions?limit=10", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
		}
		var resp gen.TransactionListResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(resp.Items) != 1 {
			t.Fatalf("items length = %d, want 1", len(resp.Items))
		}
		if resp.Items[0].OperationId != opID {
			t.Errorf("OperationId = %v, want %v", resp.Items[0].OperationId, opID)
		}
		if resp.Items[0].Reason != gen.ERRANDRESERVATION {
			t.Errorf("Reason = %v, want ERRAND_RESERVATION", resp.Items[0].Reason)
		}
		if resp.NextCursor == nil || *resp.NextCursor != nextCursor {
			t.Errorf("NextCursor = %v, want %v", resp.NextCursor, nextCursor)
		}
	})

	t.Run("returns 400 INVALID_REQUEST on malformed cursor", func(t *testing.T) {
		repo := &mockReadRepo{
			listTransactionsFn: func(ctx context.Context, userID uuid.UUID, limit int, cursor *string) (*credit.PaginatedTransactions, error) {
				return nil, credit.ErrInvalidRequest
			},
		}
		router, _, iss := setupTestRouter(t, repo, nil, nil)
		token := iss.mint(t, tokenOpts{subject: testUserID.String()})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/credits/accounts/me/transactions?cursor=badcursor", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
		var errResp gen.ErrorResponse
		if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if errResp.Code != gen.INVALIDREQUEST {
			t.Fatalf("code = %v, want INVALID_REQUEST", errResp.Code)
		}
	})
}

func TestCreateReservation(t *testing.T) {
	errandID := uuid.New()
	requesterID := uuid.New()

	t.Run("rejects missing bearer token", func(t *testing.T) {
		router, _, _ := setupTestRouter(t, nil, nil, nil)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/credits/reservations", bytes.NewBufferString(`{"errandId":"`+errandID.String()+`","amount":25}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("rejects admin role with 403 Forbidden", func(t *testing.T) {
		router, _, iss := setupTestRouter(t, nil, nil, nil)
		adminToken := iss.mint(t, tokenOpts{subject: requesterID.String(), role: "ADMIN"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/credits/reservations", bytes.NewBufferString(`{"errandId":"`+errandID.String()+`","amount":25}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})

	t.Run("rejects non-json content type", func(t *testing.T) {
		router, _, iss := setupTestRouter(t, nil, nil, nil)
		token := iss.mint(t, tokenOpts{subject: requesterID.String(), role: "STUDENT"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/credits/reservations", bytes.NewBufferString("hello"))
		req.Header.Set("Content-Type", "text/plain")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("rejects duplicate JSON keys", func(t *testing.T) {
		router, _, iss := setupTestRouter(t, nil, nil, nil)
		token := iss.mint(t, tokenOpts{subject: requesterID.String(), role: "STUDENT"})
		body := `{"errandId":"` + errandID.String() + `","amount":25,"amount":30}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/credits/reservations", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("returns 201 Created for new reservation", func(t *testing.T) {
		resID := uuid.New()
		repo := &mockReservationRepo{
			createReservationFn: func(ctx context.Context, eID, rID uuid.UUID, amt int64) (*credit.Reservation, bool, error) {
				return &credit.Reservation{
					ID:          resID,
					ErrandID:    eID,
					RequesterID: rID,
					Amount:      amt,
					Status:      credit.ReservationStatusReserved,
					CreatedAt:   time.Now().UTC(),
					UpdatedAt:   time.Now().UTC(),
				}, true, nil
			},
		}

		router, _, iss := setupTestRouter(t, nil, repo, nil)
		token := iss.mint(t, tokenOpts{subject: requesterID.String(), role: "STUDENT"})
		payload := map[string]any{
			"errandId": errandID.String(),
			"amount":   25,
		}
		data, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/credits/reservations", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body.String())
		}
		var resp gen.ReservationResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Id != resID || resp.Amount != 25 || resp.Status != gen.RESERVED {
			t.Errorf("unexpected response: %+v", resp)
		}
	})

	t.Run("returns 200 OK for identical retry", func(t *testing.T) {
		resID := uuid.New()
		repo := &mockReservationRepo{
			createReservationFn: func(ctx context.Context, eID, rID uuid.UUID, amt int64) (*credit.Reservation, bool, error) {
				return &credit.Reservation{
					ID:          resID,
					ErrandID:    eID,
					RequesterID: rID,
					Amount:      amt,
					Status:      credit.ReservationStatusReserved,
					CreatedAt:   time.Now().UTC(),
					UpdatedAt:   time.Now().UTC(),
				}, false, nil
			},
		}

		router, _, iss := setupTestRouter(t, nil, repo, nil)
		token := iss.mint(t, tokenOpts{subject: requesterID.String(), role: "STUDENT"})
		payload := map[string]any{
			"errandId": errandID.String(),
			"amount":   25,
		}
		data, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/credits/reservations", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("returns 422 INSUFFICIENT_CREDITS when requester lacks balance", func(t *testing.T) {
		repo := &mockReservationRepo{
			createReservationFn: func(ctx context.Context, eID, rID uuid.UUID, amt int64) (*credit.Reservation, bool, error) {
				return nil, false, credit.ErrInsufficientCredits
			},
		}

		router, _, iss := setupTestRouter(t, nil, repo, nil)
		token := iss.mint(t, tokenOpts{subject: requesterID.String(), role: "STUDENT"})
		payload := map[string]any{
			"errandId": errandID.String(),
			"amount":   50,
		}
		data, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/credits/reservations", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422; body: %s", rec.Code, rec.Body.String())
		}
		var errResp gen.ErrorResponse
		if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if errResp.Code != gen.INSUFFICIENTCREDITS {
			t.Fatalf("code = %v, want INSUFFICIENT_CREDITS", errResp.Code)
		}
	})
}

func TestAdjustReservationReward(t *testing.T) {
	errandID := uuid.New()
	requesterID := uuid.New()
	operationID := uuid.New()
	resID := uuid.New()

	t.Run("rejects missing bearer token", func(t *testing.T) {
		router, _, _ := setupTestRouter(t, nil, nil, nil)
		payload := map[string]any{"newReward": 35}
		data, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/credits/reservations/"+errandID.String(), bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("rejects admin role with 403 Forbidden", func(t *testing.T) {
		router, _, iss := setupTestRouter(t, nil, nil, nil)
		adminToken := iss.mint(t, tokenOpts{subject: requesterID.String(), role: "ADMIN"})
		payload := map[string]any{"newReward": 35}
		data, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/credits/reservations/"+errandID.String(), bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})

	t.Run("returns 200 OK on successful reward adjustment via /api/v1/credits/reservations/{errandId}", func(t *testing.T) {
		repo := &mockReservationRepo{
			adjustRewardFn: func(ctx context.Context, eID, rID uuid.UUID, next int64, opID uuid.UUID) (*credit.OperationResult, *credit.Reservation, error) {
				return &credit.OperationResult{
					OperationID: opID,
					ErrandID:    eID,
					OutcomeCode: credit.OutcomeSuccess,
				}, &credit.Reservation{
					ID:          resID,
					ErrandID:    eID,
					RequesterID: rID,
					Amount:      next,
					Status:      credit.ReservationStatusReserved,
					CreatedAt:   time.Now().UTC(),
					UpdatedAt:   time.Now().UTC(),
				}, nil
			},
		}

		router, _, iss := setupTestRouter(t, nil, repo, nil)
		token := iss.mint(t, tokenOpts{subject: requesterID.String(), role: "STUDENT"})
		payload := map[string]any{
			"newReward": 35,
		}
		data, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/credits/reservations/"+errandID.String(), bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Request-Id", operationID.String())
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
		}
		var resp gen.ReservationResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Amount != 35 {
			t.Errorf("Amount = %d, want 35", resp.Amount)
		}
	})

	t.Run("returns 404 on dropped internal alias /internal/v1/credit-reservations/{errandId}", func(t *testing.T) {
		router, _, iss := setupTestRouter(t, nil, nil, nil)
		token := iss.mint(t, tokenOpts{subject: requesterID.String(), role: "STUDENT"})
		payload := map[string]any{"newReward": 35}
		data, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPatch, "/internal/v1/credit-reservations/"+errandID.String(), bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("returns 404 RESERVATION_NOT_FOUND when reservation does not exist or belongs to another user", func(t *testing.T) {
		repo := &mockReservationRepo{
			adjustRewardFn: func(ctx context.Context, eID, rID uuid.UUID, next int64, opID uuid.UUID) (*credit.OperationResult, *credit.Reservation, error) {
				return nil, nil, credit.ErrReservationNotFound
			},
		}

		router, _, iss := setupTestRouter(t, nil, repo, nil)
		token := iss.mint(t, tokenOpts{subject: requesterID.String(), role: "STUDENT"})
		payload := map[string]any{
			"newReward": 35,
		}
		data, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/credits/reservations/"+errandID.String(), bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
		var errResp gen.ErrorResponse
		if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if errResp.Code != gen.RESERVATIONNOTFOUND {
			t.Fatalf("code = %v, want RESERVATION_NOT_FOUND", errResp.Code)
		}
	})
}
