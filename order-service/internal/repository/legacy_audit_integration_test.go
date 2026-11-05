//go:build integration

// AI Assistance Disclosure:
// Tool: Codex (model: GPT-6), date: 2026-10-28
// Scope: Regression checks for pre-existing active-order query plans on a
//   history-heavy database and upgrade preservation of ordinary orders,
//   history and queued refunds, using real PostgreSQL.
// Author review: PENDING

package repository

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"foc/order-service/internal/order"
)

func TestLegacyAuditMigrationPreservesExistingOrders(t *testing.T) {
	dbURL := startPostgres(t)
	oldMigrations := t.TempDir()
	for _, name := range []string{"000001_create_orders.up.sql", "000001_create_orders.down.sql"} {
		data, err := os.ReadFile(filepath.Join("../../migrations", name))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(oldMigrations, name), data, 0600))
	}
	require.NoError(t, Migrate(dbURL, oldMigrations))
	pool, err := pgxpool.New(t.Context(), dbURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	e := &env{dbURL: dbURL, pool: pool, repo: NewPostgres(pool)}
	requester, courier := uuid.New(), uuid.New()
	var originals []order.Order
	for i, state := range allStates() {
		o := inState(openErrand(requester, at(0)), state, courier, at(time.Duration(i)*time.Minute))
		_, err := pool.Exec(t.Context(), `INSERT INTO orders
			(id, requester_id, courier_id, supplier_id, supplier_name, supplier_location,
			 item_description, delivery_location, delivery_note, reward, state, version,
			 created_at, updated_at, state_changed_at, expires_at, confirm_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
			o.ID, o.RequesterID, o.CourierID, o.Supplier.ID, o.Supplier.Name, o.Supplier.Location,
			o.ItemDescription, o.DeliveryLocation, o.DeliveryNote, o.Reward, string(o.State), o.Version,
			o.CreatedAt, o.UpdatedAt, o.StateChangedAt, o.ExpiresAt, o.ConfirmBy)
		require.NoError(t, err)
		originals = append(originals, o)
	}
	history := order.HistoryEntry{OrderID: originals[0].ID, Action: order.ActionCreate,
		ToState: order.StateOpen, ActorKind: order.ActorRequester, ActorID: &requester,
		OccurredAt: at(0), CorrelationID: "legacy-before-upgrade"}
	e.insertHistory(t, history)
	guard := order.OutboxEvent{EventID: uuid.New(), RoutingKey: order.RoutingFailed,
		Payload: order.EventPayload{EventID: uuid.New(), ErrandID: uuid.New(), RequesterID: requester,
			Amount: 5, OccurredAt: at(0)}, AvailableAt: at(time.Minute), CorrelationID: "legacy-guard"}
	require.NoError(t, e.repo.QueueEvent(t.Context(), guard))
	require.NoError(t, Migrate(dbURL, "../../migrations"))
	for _, want := range originals {
		got, err := e.repo.Get(t.Context(), want.ID)
		require.NoError(t, err)
		assert.Equal(t, want, got, "the upgrade must preserve the existing %s order", want.State)
	}
	entries, err := e.repo.History(t.Context(), originals[0].ID)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, history.CorrelationID, entries[0].CorrelationID)
	var events int
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM outbox WHERE event_id=$1`, guard.EventID).Scan(&events))
	assert.Equal(t, 1, events, "an upgrade must preserve queued refunds")
}

func TestLegacyAuditActiveQueriesKeepSparseIndexes(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	// No scheduled rows: the old admin and supplier-count workload alone
	// must still be able to use its existing partial indexes.
	_, err := e.pool.Exec(ctx, `INSERT INTO orders
		(id, requester_id, supplier_id, supplier_name, supplier_location,
		 item_description, delivery_location, reward, state, created_at,
		 updated_at, state_changed_at)
		SELECT md5(i::text)::uuid, $1, $2, 'Coffee', 'COM3', 'Coffee',
		 'COM3 Lobby', 5, 'CANCELLED', $3, $3, $3
		FROM generate_series(1, 20000) AS i`, uuid.New(), uuid.New(), at(0))
	require.NoError(t, err)
	o := openErrand(uuid.New(), at(time.Minute))
	e.insert(t, o)
	_, err = e.pool.Exec(ctx, `ANALYZE orders`)
	require.NoError(t, err)

	for _, tc := range []struct {
		name  string
		query string
		args  []any
	}{
		{"admin active listing", listActiveSQL, []any{order.AdminListLimit}},
		{"supplier unfinished count", countActiveBySupplierSQL, []any{o.Supplier.ID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := func(query string) string {
				rows, err := e.pool.Query(ctx, "EXPLAIN "+query, tc.args...)
				require.NoError(t, err)
				defer rows.Close()
				var lines []string
				for rows.Next() {
					var line string
					require.NoError(t, rows.Scan(&line))
					lines = append(lines, line)
				}
				require.NoError(t, rows.Err())
				return strings.Join(lines, "\n")
			}
			// Restore just the pre-scheduling predicate for a same-data control.
			before := plan(strings.ReplaceAll(tc.query, "'SCHEDULED', ", ""))
			after := plan(tc.query)
			t.Logf("Pre-scheduling predicate:\n%s\nCurrent predicate:\n%s", before, after)
			require.NotContains(t, before, "Seq Scan on orders", "the control must use a sparse index")
			assert.NotContains(t, after, "Seq Scan on orders", "adding scheduling must preserve the old sparse-query access path")
		})
	}
}
