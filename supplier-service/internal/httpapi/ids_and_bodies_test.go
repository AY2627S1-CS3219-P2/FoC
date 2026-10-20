// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-10-20
// Scope: Table-driven tests through NewRouter of a malformed id (404, and a
//   DELETE never reaching order-service), the 64 KiB body cap (413) and a
//   missing is_available being false.
// Author review: PENDING — <reviewer to complete>

package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/supplier-service/internal/httpapi"
	"foc/supplier-service/internal/orderclient"
	"foc/supplier-service/internal/supplier"
)

// stubRepository is an in-memory supplier.Repository that counts its writes.
type stubRepository struct {
	mu     sync.Mutex
	byID   map[string]*supplier.Supplier
	writes int
}

func newStubRepository() *stubRepository {
	return &stubRepository{byID: map[string]*supplier.Supplier{
		storedID: {ID: storedID, Name: "Cool Spot", Type: "Food", Building: "Com2", LocationDescription: "Opp LT16", IsAvailable: true},
	}}
}

func (m *stubRepository) GetByID(_ context.Context, id string) (*supplier.Supplier, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.byID[id], nil
}

func (m *stubRepository) Create(_ context.Context, s *supplier.Supplier) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writes++
	m.byID[s.ID] = s
	return nil
}

func (m *stubRepository) Update(_ context.Context, s *supplier.Supplier) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writes++
	m.byID[s.ID] = s
	return nil
}

func (m *stubRepository) SoftDelete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writes++
	delete(m.byID, id)
	return nil
}

func (m *stubRepository) List(context.Context, supplier.ListFilter) ([]*supplier.Supplier, error) {
	return nil, nil
}

func (m *stubRepository) ExistsDuplicate(context.Context, string, string, string, string) (bool, error) {
	return false, nil
}

func (m *stubRepository) Count(context.Context) (int, error) { return len(m.byID), nil }

const validBody = `{"name":"Test Stall","type":"cafe","building":"COM1","location_description":"Level 2","latitude":1.29,"longitude":103.77`

// send serves one request as an admin with a signed token, through a router
// over repo and a fake order-service, and returns the response and what order-service was asked.
func send(t *testing.T, repo *stubRepository, method, path, body string) (*httptest.ResponseRecorder, []seenRequest) {
	t.Helper()
	orders, srv := startOrderService(t, http.StatusOK, activeErrands(0))
	router := httpapi.NewRouter(supplier.NewService(repo, orderclient.New(srv.URL)),
		startKeySet(t, http.StatusOK), &fakeRevocations{})
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+mintToken(t, tokenSpec{}))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec, orders.requests()
}

func TestAMalformedIDIs404(t *testing.T) {
	for _, id := range []string{"not-a-uuid", "12345", "3f2b6c1e-8a4d-4c39-9f0e-5b7d2a1c6e4"} {
		for _, m := range []struct{ method, body string }{
			{http.MethodGet, ""},
			{http.MethodPut, validBody + "}"},
			{http.MethodDelete, ""},
		} {
			t.Run(m.method+" "+id, func(t *testing.T) {
				repo := newStubRepository()

				rec, asked := send(t, repo, m.method, "/suppliers/"+id, m.body)

				assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
				assert.JSONEq(t, `{"error":"supplier not found"}`, rec.Body.String())
				assert.Empty(t, asked, "order-service must not be asked about an id that cannot exist")
				assert.Zero(t, repo.writes)
			})
		}
	}
}

func TestAnUpperCaseIDIsTheSameSupplier(t *testing.T) {
	rec, _ := send(t, newStubRepository(), http.MethodGet, "/suppliers/"+strings.ToUpper(storedID), "")

	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), storedID)
}

func TestABodyOverTheCapIs413(t *testing.T) {
	huge := validBody + `,"description":"` + strings.Repeat("x", 70<<10) + `"}`
	for _, m := range []struct{ method, path string }{
		{http.MethodPost, "/suppliers"},
		{http.MethodPut, "/suppliers/" + storedID},
	} {
		t.Run(m.method, func(t *testing.T) {
			repo := newStubRepository()

			rec, _ := send(t, repo, m.method, m.path, huge)

			assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
			assert.JSONEq(t, `{"error":"request body too large"}`, rec.Body.String())
			assert.Zero(t, repo.writes, "nothing is saved")
		})
	}
}

func TestAValidBodyFollowedByHugeWhitespaceIs413(t *testing.T) {
	// A decoder that stops after the first JSON value would save this supplier
	// and answer 201 or 200: the padding after it would never be read.
	padded := validBody + "}" + strings.Repeat(" ", 70<<10)
	for _, m := range []struct{ method, path string }{
		{http.MethodPost, "/suppliers"},
		{http.MethodPut, "/suppliers/" + storedID},
	} {
		t.Run(m.method, func(t *testing.T) {
			repo := newStubRepository()

			rec, _ := send(t, repo, m.method, m.path, padded)

			assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())
			assert.JSONEq(t, `{"error":"request body too large"}`, rec.Body.String())
			assert.Zero(t, repo.writes, "nothing is saved")
		})
	}
}

func TestSmallTrailingWhitespaceIsFine(t *testing.T) {
	rec, _ := send(t, newStubRepository(), http.MethodPost, "/suppliers", validBody+"}\n  \n")

	assert.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
}

func TestABodyUnderTheCapStillWorks(t *testing.T) {
	// 60 KiB of description is over any real supplier but under the cap.
	body := validBody + `,"description":"` + strings.Repeat("x", 60<<10) + `"}`

	rec, _ := send(t, newStubRepository(), http.MethodPost, "/suppliers", body)

	assert.Equal(t, http.StatusCreated, rec.Code, rec.Body.String()[:min(200, rec.Body.Len())])
}

func TestAnInvalidBodyIsStill400(t *testing.T) {
	for _, body := range []string{"", "{", "not json", `{"name": 3}`, validBody + "}garbage", validBody + "}" + validBody + "}"} {
		rec, _ := send(t, newStubRepository(), http.MethodPost, "/suppliers", body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, "body %q", body)
		assert.JSONEq(t, `{"error":"invalid JSON body"}`, rec.Body.String())
	}
}

func TestIsAvailableIsFalseUnlessTheBodySaysTrue(t *testing.T) {
	cases := []struct {
		name, body string
		want       bool
	}{
		{"missing", validBody + "}", false},
		{"true", validBody + `,"is_available":true}`, true},
		{"false", validBody + `,"is_available":false}`, false},
	}
	for _, tc := range cases {
		for _, m := range []struct {
			method, path string
			want         int
		}{
			{http.MethodPost, "/suppliers", http.StatusCreated},
			{http.MethodPut, "/suppliers/" + storedID, http.StatusOK},
		} {
			t.Run(tc.name+" "+m.method, func(t *testing.T) {
				repo := newStubRepository()

				rec, _ := send(t, repo, m.method, m.path, tc.body)

				require.Equal(t, m.want, rec.Code, rec.Body.String())
				assert.Contains(t, rec.Body.String(), `"is_available":`+map[bool]string{true: "true", false: "false"}[tc.want])
			})
		}
	}
}
