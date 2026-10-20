//go:build e2e

// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-10-20
// Scope: The e2e checks of supplier-service's admin writes through the whole
//   stack (D-041, D-054, D-071): an admin manages a supplier, a student cannot
//   write one, a supplier an unfinished errand names is kept (F2.2.3, D-052),
//   and a logged-out admin's token is refused.
// Author review: PENDING — <reviewer to complete>

package e2e

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// supplierBody is supplier-service's create and update body (D-036). The
// suite always sends is_available: a missing value is false (D-072).
type supplierBody struct {
	Name                string  `json:"name"`
	Type                string  `json:"type"`
	Building            string  `json:"building"`
	Floor               string  `json:"floor"`
	LocationDescription string  `json:"location_description"`
	Latitude            float64 `json:"latitude"`
	Longitude           float64 `json:"longitude"`
	OpeningTime         string  `json:"opening_time"`
	ClosingTime         string  `json:"closing_time"`
	ImageURL            string  `json:"image_url"`
	Description         string  `json:"description"`
	IsAvailable         bool    `json:"is_available"`
}

// supplierDoc is what supplier-service answers for one supplier.
type supplierDoc struct {
	ID string `json:"id"`
	supplierBody
}

// errorBody is the {"error": ...} envelope services answer with (D-036).
type errorBody struct {
	Error string `json:"error"`
}

// testSupplier is a supplier no other test or run shares: its name starts
// with e2e- (availableSupplier skips those) and its location is random, so the
// duplicate check never matches another run's.
func testSupplier() supplierBody {
	suffix := randomHex(4)
	return supplierBody{
		Name:                "e2e-" + suffix,
		Type:                "e2e",
		Building:            "E2E Block",
		Floor:               "1",
		LocationDescription: "e2e " + suffix,
		Latitude:            1.2966,
		Longitude:           103.7764,
		OpeningTime:         "09:00",
		ClosingTime:         "18:00",
		Description:         "created by the e2e suite",
		IsAvailable:         true,
	}
}

// createSupplier posts body as admin, expects 201 and registers a cleanup that
// deletes the supplier as an admin who is signed in at that point, so a test
// that logged its admin out still cleans up. A 404 there is fine: the test may
// have deleted it, and a 409 means an errand the test left open still names it.
func (s *stack) createSupplier(t *testing.T, admin user, body supplierBody) supplierDoc {
	t.Helper()
	r := s.send(t, http.MethodPost, suppliersPath, admin.token, body)
	expectStatus(t, r, http.StatusCreated)
	var doc supplierDoc
	require.NoError(t, r.decode(&doc))
	require.NotEmpty(t, doc.ID)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*requestTimeout)
		defer cancel()
		fresh, err := s.login(ctx, s.cfg.adminUsername, s.cfg.adminPassword)
		if err != nil {
			t.Logf("cleanup of supplier %s: %v", doc.ID, err)
			return
		}
		if _, err := s.request(ctx, http.MethodDelete, suppliersPath+"/"+doc.ID, fresh.token, nil, nil); err != nil {
			t.Logf("cleanup of supplier %s: %v", doc.ID, err)
		}
	})
	return doc
}

// getSupplier reads one supplier as u, expecting the wanted status.
func (s *stack) getSupplier(t *testing.T, u user, id string, want int) (supplierDoc, response) {
	t.Helper()
	r := s.send(t, http.MethodGet, suppliersPath+"/"+id, u.token, nil)
	expectStatus(t, r, want)
	var doc supplierDoc
	if want == http.StatusOK {
		require.NoError(t, r.decode(&doc))
	}
	return doc, r
}

// searchSupplierNames returns the names supplier-service's list answers for q.
func (s *stack) searchSupplierNames(t *testing.T, u user, q string) []string {
	t.Helper()
	r := s.send(t, http.MethodGet, suppliersPath+"?q="+url.QueryEscape(q), u.token, nil)
	expectStatus(t, r, http.StatusOK)
	var docs []supplierDoc
	require.NoError(t, r.decode(&docs))
	names := make([]string, 0, len(docs))
	for _, d := range docs {
		names = append(names, d.Name)
	}
	return names
}

// TestAdminManagesASupplier walks one supplier through create, read, update
// and delete as an ADMIN, through the gateway and into supplier-service's
// database, then expects it gone from reads and from search.
func TestAdminManagesASupplier(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	admin := s.admin(t)
	body := testSupplier()

	created := s.createSupplier(t, admin, body)
	assert.True(t, created.IsAvailable)
	got, _ := s.getSupplier(t, admin, created.ID, http.StatusOK)
	assert.Equal(t, body, got.supplierBody, "a read returns what was sent")
	assert.Contains(t, s.searchSupplierNames(t, admin, body.Name), body.Name)

	body.Description = "changed by the e2e suite"
	body.IsAvailable = false
	r := s.send(t, http.MethodPut, suppliersPath+"/"+created.ID, admin.token, body)
	expectStatus(t, r, http.StatusOK)
	got, _ = s.getSupplier(t, admin, created.ID, http.StatusOK)
	assert.Equal(t, body, got.supplierBody, "the update shows")

	expectStatus(t, s.send(t, http.MethodDelete, suppliersPath+"/"+created.ID, admin.token, nil), http.StatusNoContent)
	s.getSupplier(t, admin, created.ID, http.StatusNotFound)
	assert.NotContains(t, s.searchSupplierNames(t, admin, body.Name), body.Name, "a deleted supplier is not listed")

	// A string that is not a UUID names no supplier (#35).
	_, r = s.getSupplier(t, admin, "not-a-uuid", http.StatusNotFound)
	var e errorBody
	require.NoError(t, r.decode(&e))
	assert.Equal(t, "supplier not found", e.Error)
}

// TestACreateThatLeavesOutIsAvailableIsUnavailable pins D-072: a body with no
// is_available makes an unavailable supplier, and the frontend's form is why
// that does not surprise anyone, because it always sends the field.
func TestACreateThatLeavesOutIsAvailableIsUnavailable(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	admin := s.admin(t)
	body := testSupplier()
	// A map, not supplierBody, so is_available is absent from the JSON.
	raw := map[string]any{
		"name": body.Name, "type": body.Type, "building": body.Building,
		"location_description": body.LocationDescription, "latitude": body.Latitude, "longitude": body.Longitude,
	}

	r := s.send(t, http.MethodPost, suppliersPath, admin.token, raw)

	expectStatus(t, r, http.StatusCreated)
	var doc supplierDoc
	require.NoError(t, r.decode(&doc))
	t.Cleanup(func() { s.deleteSupplierBestEffort(doc.ID) })
	assert.False(t, doc.IsAvailable)
}

// deleteSupplierBestEffort deletes a supplier as a freshly signed-in admin and
// ignores the answer; it is for a cleanup that has no supplierDoc from createSupplier.
func (s *stack) deleteSupplierBestEffort(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*requestTimeout)
	defer cancel()
	fresh, err := s.login(ctx, s.cfg.adminUsername, s.cfg.adminPassword)
	if err != nil {
		return
	}
	_, _ = s.request(ctx, http.MethodDelete, suppliersPath+"/"+id, fresh.token, nil, nil)
}

// TestStudentCannotWriteSuppliers expects a STUDENT's create, update and
// delete to be refused with 403, also when the request forges the identity
// headers the gateway discards, and the supplier to be unchanged afterwards.
func TestStudentCannotWriteSuppliers(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	admin, student := s.admin(t), s.newStudent(t)
	body := testSupplier()
	created := s.createSupplier(t, admin, body)
	path := suppliersPath + "/" + created.ID

	forged := http.Header{}
	forged.Set("X-User-Role", "ADMIN")
	forged.Set("X-User-Id", admin.id)
	attempts := []struct {
		name, method, path string
		body               any
	}{
		{"create", http.MethodPost, suppliersPath, testSupplier()},
		{"update", http.MethodPut, path, supplierBody{Name: "e2e-renamed", Type: "e2e", Building: "E2E Block", LocationDescription: "e2e renamed", IsAvailable: true}},
		{"delete", http.MethodDelete, path, nil},
	}
	for _, a := range attempts {
		for name, header := range map[string]http.Header{"": nil, " with forged identity headers": forged} {
			r, err := s.request(t.Context(), a.method, a.path, student.token, a.body, header)
			require.NoError(t, err)
			if r.status != http.StatusForbidden {
				t.Errorf("a student's %s%s: %s; want 403", a.name, name, r)
			}
		}
	}

	got, _ := s.getSupplier(t, admin, created.ID, http.StatusOK)
	assert.Equal(t, body, got.supplierBody, "nothing the student sent was saved")
}

// TestSupplierWithUnfinishedErrandIsKept expects supplier-service to refuse a
// delete while an errand that is not finished names the supplier (F2.2.3),
// and to allow it once the errand is cancelled, the errand keeping the
// supplier's name it copied (D-052).
func TestSupplierWithUnfinishedErrandIsKept(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	admin, requester := s.admin(t), s.newStudent(t)
	body := testSupplier()
	sup := s.createSupplier(t, admin, body)
	path := suppliersPath + "/" + sup.ID

	// Built by hand: errandInput skips the suppliers these tests make.
	e := s.createErrand(t, requester, createErrandRequest{
		SupplierID:       sup.ID,
		ItemDescription:  "e2e " + t.Name(),
		DeliveryLocation: s.deliveryLocation(t, requester),
		Reward:           5,
	}).Errand

	// Straight after the create: on a stack with T in seconds the errand
	// expires and stops counting soon.
	r := s.send(t, http.MethodDelete, path, admin.token, nil)
	expectStatus(t, r, http.StatusConflict)
	var refusal errorBody
	require.NoError(t, r.decode(&refusal))
	assert.Equal(t, "supplier is referenced by an errand that is not finished", refusal.Error)
	s.getSupplier(t, admin, sup.ID, http.StatusOK)

	// The cancel answers 409 when the errand has already expired; either way
	// the errand is finished and the delete must go through.
	cancel := s.act(t, requester, e.ID, "cancel")
	expectStatus(t, cancel, http.StatusOK, http.StatusConflict)
	if cancel.status == http.StatusConflict {
		require.Equal(t, stateExpired, s.errand(t, requester, e.ID).Errand.State, "a refused cancel means the errand expired: %s", cancel)
	}

	expectStatus(t, s.send(t, http.MethodDelete, path, admin.token, nil), http.StatusNoContent)
	s.getSupplier(t, admin, sup.ID, http.StatusNotFound)
	assert.Equal(t, body.Name, s.errand(t, requester, e.ID).Errand.Supplier.Name,
		"the errand keeps the name of the supplier it was created with")
}

// TestLoggedOutAdminCannotWriteSuppliers logs an admin out and expects the
// access token they still hold, not yet expired, to be refused on a supplier
// write (D-054, D-071). Only the status is asserted: the gateway answers once
// it checks revocation too, and supplier-service answers until then, in
// different words.
func TestLoggedOutAdminCannotWriteSuppliers(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	admin := s.admin(t)
	body := testSupplier()
	expectStatus(t, s.send(t, http.MethodGet, suppliersPath, admin.token, nil), http.StatusOK)

	s.logout(t, admin)

	r := s.send(t, http.MethodPost, suppliersPath, admin.token, body)
	expectStatus(t, r, http.StatusUnauthorized)
	fresh := s.admin(t)
	assert.NotContains(t, s.searchSupplierNames(t, fresh, body.Name), body.Name, "the refused write created nothing")
}
