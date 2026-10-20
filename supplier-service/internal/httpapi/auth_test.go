// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-10-20
// Scope: Table-driven tests of the token check on POST, PUT and DELETE
//   /suppliers through NewRouter: the 401, 403 and 503 answers, forged identity
//   headers, and reads and /health needing no token (D-041, D-054).
// Author review: PENDING — <reviewer to complete>

package httpapi_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/supplier-service/internal/auth"
	"foc/supplier-service/internal/httpapi"
	"foc/supplier-service/internal/orderclient"
	"foc/supplier-service/internal/supplier"
)

const supplierBody = `{"name":"Test Stall","type":"cafe","building":"COM1","location_description":"Level 2","latitude":1.29,"longitude":103.77}`

// memRepository is an in-memory supplier.Repository that counts writes, so a
// refused request can be shown to have changed nothing.
type memRepository struct {
	mu     sync.Mutex
	byID   map[string]*supplier.Supplier
	writes int
}

func newMemRepository() *memRepository {
	return &memRepository{byID: map[string]*supplier.Supplier{
		storedID: {ID: storedID, Name: "Cool Spot", Type: "Food", Building: "Com2", LocationDescription: "Opp LT16", IsAvailable: true},
	}}
}

func (m *memRepository) GetByID(_ context.Context, id string) (*supplier.Supplier, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.byID[id], nil
}

func (m *memRepository) Create(_ context.Context, s *supplier.Supplier) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writes++
	m.byID[s.ID] = s
	return nil
}

func (m *memRepository) Update(_ context.Context, s *supplier.Supplier) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writes++
	m.byID[s.ID] = s
	return nil
}

func (m *memRepository) SoftDelete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writes++
	delete(m.byID, id)
	return nil
}

func (m *memRepository) List(context.Context, supplier.ListFilter) ([]*supplier.Supplier, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*supplier.Supplier, 0, len(m.byID))
	for _, s := range m.byID {
		out = append(out, s)
	}
	return out, nil
}

func (m *memRepository) ExistsDuplicate(context.Context, string, string, string, string) (bool, error) {
	return false, nil
}

func (m *memRepository) Count(context.Context) (int, error) { return len(m.byID), nil }

type zeroErrands struct{ asked int }

func (z *zeroErrands) ActiveErrands(context.Context, string) (int, error) {
	z.asked++
	return 0, nil
}

func TestWritesNeedAnAdminToken(t *testing.T) {
	adminSub := uuid.New()
	revoked := uuid.NewString()
	student := mintToken(t, tokenSpec{role: "STUDENT"})
	admin := mintToken(t, tokenSpec{sub: adminSub})
	with := func(edit func(jwt.MapClaims)) string { return mintToken(t, tokenSpec{edit: edit}) }

	cases := []struct {
		name          string
		authorization []string // the Authorization header values; none for no header
		extra         map[string]string
		redisErr      error
		keysStatus    int
		revokedJTI    string
		wantStatus    int
		wantBody      string
		wantChanged   bool
	}{
		{name: "admin token", authorization: []string{"Bearer " + admin}, wantStatus: http.StatusCreated, wantChanged: true},
		{name: "scheme in any case", authorization: []string{"bEaReR " + admin}, wantStatus: http.StatusCreated, wantChanged: true},
		{name: "student token", authorization: []string{"Bearer " + student}, wantStatus: http.StatusForbidden, wantBody: `{"error":"admin role required"}`},
		{
			name: "student token plus a forged X-User-Role: ADMIN", authorization: []string{"Bearer " + student},
			extra:      map[string]string{"X-User-Role": "ADMIN", "X-User-Id": adminSub.String()},
			wantStatus: http.StatusForbidden, wantBody: `{"error":"admin role required"}`,
		},
		{
			name:       "no token plus a forged X-User-Role: ADMIN",
			extra:      map[string]string{"X-User-Role": "ADMIN", "X-User-Id": adminSub.String()},
			wantStatus: http.StatusUnauthorized, wantBody: `{"error":"missing, invalid or expired access token"}`,
		},
		{name: "no Authorization header", wantStatus: http.StatusUnauthorized, wantBody: `{"error":"missing, invalid or expired access token"}`},
		{name: "Basic scheme", authorization: []string{"Basic " + admin}, wantStatus: http.StatusUnauthorized},
		{name: "Bearer with no token", authorization: []string{"Bearer"}, wantStatus: http.StatusUnauthorized},
		{name: "Bearer with an empty token", authorization: []string{"Bearer "}, wantStatus: http.StatusUnauthorized},
		{name: "a space inside the token", authorization: []string{"Bearer " + admin + " extra"}, wantStatus: http.StatusUnauthorized},
		{name: "two Authorization headers", authorization: []string{"Bearer " + admin, "Bearer " + admin}, wantStatus: http.StatusUnauthorized},
		{name: "garbage token", authorization: []string{"Bearer not-a-token"}, wantStatus: http.StatusUnauthorized},
		{
			name:          "expired token",
			authorization: []string{"Bearer " + with(func(c jwt.MapClaims) { c["exp"] = float64(1) })}, wantStatus: http.StatusUnauthorized,
		},
		{
			name:          "refresh token used as an access token",
			authorization: []string{"Bearer " + with(func(c jwt.MapClaims) { c["typ"] = "refresh"; delete(c, "role") })}, wantStatus: http.StatusUnauthorized,
		},
		{
			name:          "no role claim",
			authorization: []string{"Bearer " + with(func(c jwt.MapClaims) { delete(c, "role") })}, wantStatus: http.StatusUnauthorized,
		},
		{
			name:          "an unknown role",
			authorization: []string{"Bearer " + with(func(c jwt.MapClaims) { c["role"] = "SUPERUSER" })}, wantStatus: http.StatusUnauthorized,
		},
		{
			name:          "wrong issuer",
			authorization: []string{"Bearer " + with(func(c jwt.MapClaims) { c["iss"] = "someone-else" })}, wantStatus: http.StatusUnauthorized,
		},
		{
			name:          "wrong audience",
			authorization: []string{"Bearer " + with(func(c jwt.MapClaims) { c["aud"] = []string{"other"} })}, wantStatus: http.StatusUnauthorized,
		},
		{
			name:          "sub is not a UUID",
			authorization: []string{"Bearer " + with(func(c jwt.MapClaims) { c["sub"] = "admindemo" })}, wantStatus: http.StatusUnauthorized,
		},
		{
			name:          "logged out admin (jti blocklisted)",
			authorization: []string{"Bearer " + mintToken(t, tokenSpec{jti: revoked})}, revokedJTI: revoked,
			wantStatus: http.StatusUnauthorized, wantBody: `{"error":"missing, invalid or expired access token"}`,
		},
		{
			name:          "user-service's keys cannot be loaded",
			authorization: []string{"Bearer " + admin}, keysStatus: http.StatusBadGateway,
			wantStatus: http.StatusServiceUnavailable, wantBody: `{"error":"sign-in cannot be checked right now; please try again"}`,
		},
		{
			name:          "Redis cannot be reached",
			authorization: []string{"Bearer " + admin}, redisErr: fmt.Errorf("read revocation keys: %w: dial tcp: refused", auth.ErrRedisUnreachable),
			wantStatus: http.StatusServiceUnavailable, wantBody: `{"error":"sign-in cannot be checked right now; please try again"}`,
		},
		{
			name:          "Redis answers with an error",
			authorization: []string{"Bearer " + admin}, redisErr: errors.New("read revocation keys: NOAUTH Authentication required."),
			wantStatus: http.StatusServiceUnavailable,
		},
	}

	methods := []struct {
		name, method, path, body string
	}{
		{"POST", http.MethodPost, "/suppliers", supplierBody},
		{"PUT", http.MethodPut, "/suppliers/" + storedID, supplierBody},
		{"DELETE", http.MethodDelete, "/suppliers/" + storedID, ""},
	}
	for _, tc := range cases {
		for _, m := range methods {
			t.Run(tc.name+"/"+m.name, func(t *testing.T) {
				status := tc.keysStatus
				if status == 0 {
					status = http.StatusOK
				}
				repo := newMemRepository()
				errands := &zeroErrands{}
				rev := &fakeRevocations{revokedJTIs: map[string]bool{}, err: tc.redisErr}
				if tc.revokedJTI != "" {
					rev.revokedJTIs[tc.revokedJTI] = true
				}
				router := httpapi.NewRouter(supplier.NewService(repo, errands), startKeySet(t, status), rev)

				req := httptest.NewRequest(m.method, m.path, strings.NewReader(m.body))
				req.Header.Set("Content-Type", "application/json")
				for _, v := range tc.authorization {
					req.Header.Add("Authorization", v)
				}
				for k, v := range tc.extra {
					req.Header.Set(k, v)
				}
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)

				wantStatus := tc.wantStatus
				if wantStatus == http.StatusCreated && m.method != http.MethodPost {
					wantStatus = map[string]int{http.MethodPut: http.StatusOK, http.MethodDelete: http.StatusNoContent}[m.method]
				}
				assert.Equal(t, wantStatus, rec.Code, rec.Body.String())
				if tc.wantBody != "" {
					assert.JSONEq(t, tc.wantBody, rec.Body.String())
					assert.Equal(t, "application/json", rec.Header().Get("Content-Type"), "refusals are JSON, not text/plain")
				}
				if tc.wantChanged {
					assert.Positive(t, repo.writes)
				} else {
					assert.Zero(t, repo.writes, "a refused write must change nothing")
					assert.Zero(t, errands.asked, "order-service must not be asked")
				}
			})
		}
	}
}

func TestReadsAndHealthNeedNoToken(t *testing.T) {
	garbage := "Bearer not-a-token"
	for _, authorization := range []string{"", garbage} {
		for _, path := range []string{"/health", "/suppliers", "/suppliers/" + storedID} {
			t.Run(fmt.Sprintf("%q %s", authorization, path), func(t *testing.T) {
				// Keys that cannot be loaded and a Redis that is down: neither
				// is consulted for a read.
				rev := &fakeRevocations{err: auth.ErrRedisUnreachable}
				router := httpapi.NewRouter(supplier.NewService(newMemRepository(), &zeroErrands{}), startKeySet(t, http.StatusBadGateway), rev)

				req := httptest.NewRequest(http.MethodGet, path, nil)
				if authorization != "" {
					req.Header.Set("Authorization", authorization)
				}
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)

				assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			})
		}
	}
}

func TestAnAdminWriteForwardsTheVerifiedTokenAndCallerIsNeverTakenFromHeaders(t *testing.T) {
	// The Authorization value order-service gets is the token authenticate
	// verified, rebuilt as "Bearer <token>", whatever the case of the scheme
	// the client used.
	admin := mintToken(t, tokenSpec{})
	repo := newMemRepository()
	orders, srv := startOrderService(t, http.StatusOK, activeErrands(0))
	router := httpapi.NewRouter(supplier.NewService(repo, orderclient.New(srv.URL)), startKeySet(t, http.StatusOK), &fakeRevocations{})

	req := httptest.NewRequest(http.MethodDelete, "/suppliers/"+storedID, nil)
	req.Header.Set("Authorization", "bEaReR "+admin)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	seen := orders.requests()
	require.Len(t, seen, 1)
	assert.Equal(t, "Bearer "+admin, seen[0].authorization)
}
