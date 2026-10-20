// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-18
// Scope: Table-driven tests of DELETE /suppliers/{id} through NewRouter, with an
//   in-memory repository and the real order-service client against an httptest
//   order-service: 409, 204, 500, 404, the role check's 403, and the admin's
//   token reaching order-service.
// Author review: James Teo — reviewed and verified.
//
// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-10-20
// Scope: The delete cases now send signed tokens through the real verifier
//   instead of an X-User-Role header, plus a student and an anonymous caller
//   forging that header (403 and 401, order-service not asked).
// Author review: PENDING — <reviewer to complete>

package httpapi_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/supplier-service/internal/httpapi"
	"foc/supplier-service/internal/orderclient"
	"foc/supplier-service/internal/supplier"
)

const (
	storedID  = "3f2b6c1e-8a4d-4c39-9f0e-5b7d2a1c6e48"
	unknownID = "9d1e7a52-0c3b-4f68-a2d4-6e8b1f0c7a39"
)

// errNotOnDeletePath is the fake repository's answer for the methods a delete
// never calls, so a test that strays off that path fails instead of passing.
var errNotOnDeletePath = errors.New("fake repository: not on the delete path")

// fakeRepository keeps suppliers in memory and records soft deletes.
type fakeRepository struct {
	byID        map[string]*supplier.Supplier
	softDeleted []string
}

func newFakeRepository(ids ...string) *fakeRepository {
	f := &fakeRepository{byID: map[string]*supplier.Supplier{}}
	for _, id := range ids {
		f.byID[id] = &supplier.Supplier{ID: id, Name: "Cool Spot", IsAvailable: true}
	}
	return f
}

// GetByID answers nil, nil for an unknown id, as the Postgres adapter does.
func (f *fakeRepository) GetByID(_ context.Context, id string) (*supplier.Supplier, error) {
	return f.byID[id], nil
}

func (f *fakeRepository) SoftDelete(_ context.Context, id string) error {
	f.softDeleted = append(f.softDeleted, id)
	delete(f.byID, id)
	return nil
}

func (f *fakeRepository) Create(context.Context, *supplier.Supplier) error {
	return errNotOnDeletePath
}

func (f *fakeRepository) List(context.Context, supplier.ListFilter) ([]*supplier.Supplier, error) {
	return nil, errNotOnDeletePath
}

func (f *fakeRepository) Update(context.Context, *supplier.Supplier) error {
	return errNotOnDeletePath
}

func (f *fakeRepository) ExistsDuplicate(context.Context, string, string, string, string) (bool, error) {
	return false, errNotOnDeletePath
}

func (f *fakeRepository) Count(context.Context) (int, error) {
	return 0, errNotOnDeletePath
}

// seenRequest is one request the fake order-service received.
type seenRequest struct {
	path          string
	authorization string
}

// fakeOrderService records the requests it receives. Its handler runs on the
// server's goroutine, hence the mutex.
type fakeOrderService struct {
	mu   sync.Mutex
	seen []seenRequest
}

// startOrderService starts a fake order-service that answers every request
// with status and body.
func startOrderService(t *testing.T, status int, body string) (*fakeOrderService, *httptest.Server) {
	t.Helper()
	f := &fakeOrderService{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.seen = append(f.seen, seenRequest{
			path:          r.URL.EscapedPath(),
			authorization: r.Header.Get("Authorization"),
		})
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeOrderService) requests() []seenRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]seenRequest(nil), f.seen...)
}

// activeErrands is order-service's answer for the stored supplier.
func activeErrands(n int) string {
	return fmt.Sprintf(`{"supplierId":"%s","activeErrands":%d}`, storedID, n)
}

func TestDelete_AsksOrderServiceBeforeDeleting(t *testing.T) {
	// forged are headers a client invented; none of them may matter.
	type caller struct {
		authorization string
		forged        map[string]string
	}
	forgedAdmin := map[string]string{"X-User-Role": "ADMIN", "X-User-Id": "11111111-1111-4111-8111-111111111111"}
	admin := caller{authorization: "Bearer " + mintToken(t, tokenSpec{role: "ADMIN"})}
	student := caller{authorization: "Bearer " + mintToken(t, tokenSpec{role: "STUDENT"})}
	forgedByStudent := caller{authorization: student.authorization, forged: forgedAdmin}
	anonymous := caller{forged: forgedAdmin}

	tests := []struct {
		name   string
		id     string // the supplier in the URL
		caller caller

		// What order-service answers, or orderDown for one that is not running.
		orderStatus int
		orderBody   string
		orderDown   bool

		wantStatus  int
		wantBody    string // JSON; "" for an empty body
		wantReached bool   // the errand check reached order-service
		wantDeleted bool
	}{
		{
			name:        "active errands: 409, supplier kept",
			id:          storedID,
			caller:      admin,
			orderStatus: http.StatusOK,
			orderBody:   activeErrands(2),
			wantStatus:  http.StatusConflict,
			wantBody:    `{"error":"supplier is referenced by an errand that is not finished"}`,
			wantReached: true,
		},
		{
			name:        "no active errands: 204, soft-deleted",
			id:          storedID,
			caller:      admin,
			orderStatus: http.StatusOK,
			orderBody:   activeErrands(0),
			wantStatus:  http.StatusNoContent,
			wantReached: true,
			wantDeleted: true,
		},
		{
			name:        "order-service rejects the token: 500, supplier kept",
			id:          storedID,
			caller:      admin,
			orderStatus: http.StatusUnauthorized,
			orderBody:   `{"error":"invalid token"}`,
			wantStatus:  http.StatusInternalServerError,
			wantBody:    `{"error":"internal server error"}`,
			wantReached: true,
		},
		{
			name:        "order-service fails: 500, supplier kept",
			id:          storedID,
			caller:      admin,
			orderStatus: http.StatusServiceUnavailable,
			orderBody:   `{"error":"unavailable"}`,
			wantStatus:  http.StatusInternalServerError,
			wantBody:    `{"error":"internal server error"}`,
			wantReached: true,
		},
		{
			name:       "order-service unreachable: 500, supplier kept",
			id:         storedID,
			caller:     admin,
			orderDown:  true,
			wantStatus: http.StatusInternalServerError,
			wantBody:   `{"error":"internal server error"}`,
		},
		{
			name:        "unknown supplier: 404 without asking order-service",
			id:          unknownID,
			caller:      admin,
			orderStatus: http.StatusOK,
			orderBody:   activeErrands(0),
			wantStatus:  http.StatusNotFound,
			wantBody:    `{"error":"supplier not found"}`,
		},
		{
			name:        "not an admin: 403 without asking order-service",
			id:          storedID,
			caller:      student,
			orderStatus: http.StatusOK,
			orderBody:   activeErrands(0),
			wantStatus:  http.StatusForbidden,
			wantBody:    `{"error":"admin role required"}`,
		},
		{
			name:        "a student forging X-User-Role: ADMIN: 403 without asking order-service",
			id:          storedID,
			caller:      forgedByStudent,
			orderStatus: http.StatusOK,
			orderBody:   activeErrands(0),
			wantStatus:  http.StatusForbidden,
			wantBody:    `{"error":"admin role required"}`,
		},
		{
			name:        "no token, forged headers: 401 without asking order-service",
			id:          storedID,
			caller:      anonymous,
			orderStatus: http.StatusOK,
			orderBody:   activeErrands(0),
			wantStatus:  http.StatusUnauthorized,
			wantBody:    `{"error":"missing, invalid or expired access token"}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeRepository(storedID)
			orders, srv := startOrderService(t, tc.orderStatus, tc.orderBody)
			if tc.orderDown {
				srv.Close() // its port now refuses connections at once
			}
			router := httpapi.NewRouter(supplier.NewService(repo, orderclient.New(srv.URL)),
				startKeySet(t, http.StatusOK), &fakeRevocations{})

			req := httptest.NewRequest(http.MethodDelete, "/suppliers/"+tc.id, nil)
			if tc.caller.authorization != "" {
				req.Header.Set("Authorization", tc.caller.authorization)
			}
			for k, v := range tc.caller.forged {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			assert.Equal(t, tc.wantStatus, rec.Code)
			if tc.wantBody == "" {
				assert.Empty(t, rec.Body.String())
			} else {
				assert.JSONEq(t, tc.wantBody, rec.Body.String())
			}

			seen := orders.requests()
			if tc.wantReached {
				require.Len(t, seen, 1)
				assert.Equal(t, "/suppliers/"+tc.id+"/active-errands", seen[0].path)
				// order-service checks for itself that the caller is an ADMIN,
				// so it must get the admin's own Authorization header.
				assert.Equal(t, admin.authorization, seen[0].authorization)
			} else {
				assert.Empty(t, seen, "order-service must not be asked")
			}

			if tc.wantDeleted {
				assert.Equal(t, []string{tc.id}, repo.softDeleted)
			} else {
				assert.Empty(t, repo.softDeleted, "the supplier must be kept")
			}
		})
	}
}
