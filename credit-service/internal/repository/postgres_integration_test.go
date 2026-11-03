//go:build integration

// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-11-03
// Scope: Updated integration tests for 3-table schema: verified lazy wallet init on payout, ledger-backed adjustment idempotency, and removed pending tables.
// Author review: Reviewed by Seth

package repository_test

import (
	"context"
	"net"
	"testing"
	"time"

	"foc/credit-service/internal/credit"
	"foc/credit-service/internal/repository"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

type testEnv struct {
	pool *pgxpool.Pool
	repo *repository.PostgresRepository
}

func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()
	ctx := context.Background()

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:16-alpine",
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_DB":       "credit_test",
				"POSTGRES_USER":     "credit",
				"POSTGRES_PASSWORD": "secretpassword",
			},
			WaitingFor: wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(time.Minute),
		},
		Started: true,
	})
	require.NoError(t, err, "start PostgreSQL container")
	t.Cleanup(func() {
		_ = testcontainers.TerminateContainer(container)
	})

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "5432/tcp")
	require.NoError(t, err)

	dbURL := "postgres://credit:secretpassword@" + net.JoinHostPort(host, port.Port()) + "/credit_test?sslmode=disable"

	err = repository.Apply(dbURL, "file://../../migrations")
	require.NoError(t, err, "apply migrations")

	ver, dirty, err := repository.GetSchemaVersion(dbURL, "file://../../migrations")
	require.NoError(t, err)
	require.False(t, dirty)
	require.Equal(t, repository.ExpectedSchemaVersion, ver)

	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err, "create pgxpool")
	t.Cleanup(pool.Close)

	repo := repository.NewPostgresRepository(pool)
	return &testEnv{pool: pool, repo: repo}
}

func TestPostgresRepository_Integration(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	t.Run("AllocateInitialCredits creates wallet and is idempotent", func(t *testing.T) {
		userID := uuid.New()
		now := time.Now().UTC()

		// 1. Initial allocation creates account with 100 available credits
		err := env.repo.AllocateInitialCredits(ctx, userID, 100, now)
		require.NoError(t, err)

		acc, err := env.repo.GetAccount(ctx, userID)
		require.NoError(t, err)
		require.Equal(t, userID, acc.UserID)
		require.Equal(t, int64(100), acc.AvailableCredits)
		require.Equal(t, int64(0), acc.ReservedCredits)
		require.Equal(t, credit.IntegrityConsistent, acc.IntegrityStatus)

		// 2. Duplicate allocation is an idempotent no-op
		err = env.repo.AllocateInitialCredits(ctx, userID, 100, now)
		require.NoError(t, err)

		acc2, err := env.repo.GetAccount(ctx, userID)
		require.NoError(t, err)
		require.Equal(t, int64(100), acc2.AvailableCredits)

		// Verify exactly 1 ledger row exists
		var txCount int
		require.NoError(t, env.pool.QueryRow(ctx, "SELECT COUNT(*) FROM credit_transactions WHERE user_id = $1", userID).Scan(&txCount))
		require.Equal(t, 1, txCount)
	})

	t.Run("CreateReservation locks balance and enforces idempotency", func(t *testing.T) {
		userID := uuid.New()
		errandID := uuid.New()
		now := time.Now().UTC()

		require.NoError(t, env.repo.AllocateInitialCredits(ctx, userID, 100, now))

		// 1. Create reservation of 50 credits
		res, created, err := env.repo.CreateReservation(ctx, errandID, userID, 50)
		require.NoError(t, err)
		require.True(t, created)
		require.Equal(t, errandID, res.ErrandID)
		require.Equal(t, userID, res.RequesterID)
		require.Equal(t, int64(50), res.Amount)
		require.Equal(t, credit.ReservationStatusReserved, res.Status)

		acc, err := env.repo.GetAccount(ctx, userID)
		require.NoError(t, err)
		require.Equal(t, int64(50), acc.AvailableCredits)
		require.Equal(t, int64(50), acc.ReservedCredits)

		// 2. Idempotent repeat returns existing reservation
		res2, created2, err := env.repo.CreateReservation(ctx, errandID, userID, 50)
		require.NoError(t, err)
		require.False(t, created2)
		require.Equal(t, res.ID, res2.ID)

		// 3. Duplicate errandId with different amount or requester returns conflict
		_, _, err = env.repo.CreateReservation(ctx, errandID, userID, 60)
		require.ErrorIs(t, err, credit.ErrReservationConflict)
		_, _, err = env.repo.CreateReservation(ctx, errandID, uuid.New(), 50)
		require.ErrorIs(t, err, credit.ErrReservationConflict)

		// 4. Insufficient credits fails
		errand2 := uuid.New()
		_, _, err = env.repo.CreateReservation(ctx, errand2, userID, 60)
		require.ErrorIs(t, err, credit.ErrInsufficientCredits)
	})

	t.Run("AdjustReservationReward increases and decreases holds", func(t *testing.T) {
		userID := uuid.New()
		errandID := uuid.New()
		now := time.Now().UTC()

		require.NoError(t, env.repo.AllocateInitialCredits(ctx, userID, 100, now))
		_, _, err := env.repo.CreateReservation(ctx, errandID, userID, 40)
		require.NoError(t, err)

		// 1. Increase reward: 40 -> 60 (requires 20 extra credits)
		op1 := uuid.New()
		_, res, err := env.repo.AdjustReservationReward(ctx, errandID, userID, 60, 1, op1)
		require.NoError(t, err)
		require.Equal(t, int64(60), res.Amount)
		require.Equal(t, int64(2), res.Version)

		acc, err := env.repo.GetAccount(ctx, userID)
		require.NoError(t, err)
		require.Equal(t, int64(40), acc.AvailableCredits)
		require.Equal(t, int64(60), acc.ReservedCredits)

		// Repeated adjustment with same op1 is idempotent
		_, resRetry, err := env.repo.AdjustReservationReward(ctx, errandID, userID, 60, 1, op1)
		require.NoError(t, err)
		require.Equal(t, res.Amount, resRetry.Amount)

		// Reusing op1 for a different version is an idempotency conflict
		_, _, err = env.repo.AdjustReservationReward(ctx, errandID, userID, 60, 2, op1)
		require.ErrorIs(t, err, credit.ErrIdempotencyConflict)

		// 2. Decrease reward: 60 -> 25 (releases 35 credits back to available)
		op2 := uuid.New()
		_, res2, err := env.repo.AdjustReservationReward(ctx, errandID, userID, 25, 2, op2)
		require.NoError(t, err)
		require.Equal(t, int64(25), res2.Amount)
		require.Equal(t, int64(3), res2.Version)
		_, _, err = env.repo.AdjustReservationReward(ctx, errandID, userID, 20, 1, uuid.New())
		require.ErrorIs(t, err, credit.ErrReservationConflict)

		acc2, err := env.repo.GetAccount(ctx, userID)
		require.NoError(t, err)
		require.Equal(t, int64(75), acc2.AvailableCredits)
		require.Equal(t, int64(25), acc2.ReservedCredits)

		// A later amendment may legitimately reuse an earlier target, but must use a new operation ID.
		op3 := uuid.New()
		_, res3, err := env.repo.AdjustReservationReward(ctx, errandID, userID, 60, 3, op3)
		require.NoError(t, err)
		require.Equal(t, int64(60), res3.Amount)
		require.Equal(t, int64(4), res3.Version)
		acc3, err := env.repo.GetAccount(ctx, userID)
		require.NoError(t, err)
		require.Equal(t, int64(40), acc3.AvailableCredits)
		require.Equal(t, int64(60), acc3.ReservedCredits)

		// 3. Unowned reservation returns ErrReservationNotFound
		otherUser := uuid.New()
		op4 := uuid.New()
		_, _, err = env.repo.AdjustReservationReward(ctx, errandID, otherUser, 30, 1, op4)
		require.ErrorIs(t, err, credit.ErrReservationNotFound)
	})

	t.Run("AdjustReservationReward rejects insufficient credits without persisting error", func(t *testing.T) {
		userID := uuid.New()
		errandID := uuid.New()
		require.NoError(t, env.repo.AllocateInitialCredits(ctx, userID, 100, time.Now().UTC()))
		_, _, err := env.repo.CreateReservation(ctx, errandID, userID, 90)
		require.NoError(t, err)

		opID := uuid.New()
		_, _, err = env.repo.AdjustReservationReward(ctx, errandID, userID, 110, 1, opID)
		require.ErrorIs(t, err, credit.ErrInsufficientCredits)

		var amount int64
		require.NoError(t, env.pool.QueryRow(ctx, `SELECT amount FROM credit_reservations WHERE errand_id = $1`, errandID).Scan(&amount))
		require.Equal(t, int64(90), amount)
	})

	t.Run("ApplyRewardSet releases only and ignores replayed PATCH operations", func(t *testing.T) {
		userID, errandID := uuid.New(), uuid.New()
		now := time.Now().UTC()
		require.NoError(t, env.repo.AllocateInitialCredits(ctx, userID, 100, now))
		_, _, err := env.repo.CreateReservation(ctx, errandID, userID, 20)
		require.NoError(t, err)

		// An event cannot increase the secured amount, even with a newer version.
		err = env.repo.ApplyRewardSet(ctx, errandID, uuid.New(), 30, 2, now)
		require.ErrorIs(t, err, credit.ErrLifecycleViolation)
		var heldAmount, heldVersion int64
		require.NoError(t, env.pool.QueryRow(ctx, `SELECT amount, version FROM credit_reservations WHERE errand_id = $1`, errandID).Scan(&heldAmount, &heldVersion))
		require.Equal(t, int64(20), heldAmount)
		require.Equal(t, int64(1), heldVersion)

		// A PATCH can raise the hold. Its matching event is a financial duplicate.
		opID := uuid.New()
		_, res, err := env.repo.AdjustReservationReward(ctx, errandID, userID, 50, 1, opID)
		require.NoError(t, err)
		require.Equal(t, int64(2), res.Version)
		require.NoError(t, env.repo.ApplyRewardSet(ctx, errandID, opID, 50, 2, now))
		acc, err := env.repo.GetAccount(ctx, userID)
		require.NoError(t, err)
		require.Equal(t, int64(50), acc.ReservedCredits)

		// A committed decrease releases the difference once. Older and duplicate
		// versions are ignored without re-releasing credits.
		decreaseEventID := uuid.New()
		require.NoError(t, env.repo.ApplyRewardSet(ctx, errandID, decreaseEventID, 15, 3, now))
		accAfterDecrease, err := env.repo.GetAccount(ctx, userID)
		require.NoError(t, err)
		require.Equal(t, int64(85), accAfterDecrease.AvailableCredits)
		require.Equal(t, int64(15), accAfterDecrease.ReservedCredits)

		// Duplicate event does not release again
		require.NoError(t, env.repo.ApplyRewardSet(ctx, errandID, decreaseEventID, 15, 3, now))
		accAfterDup, err := env.repo.GetAccount(ctx, userID)
		require.NoError(t, err)
		require.Equal(t, int64(85), accAfterDup.AvailableCredits)
		require.Equal(t, int64(15), accAfterDup.ReservedCredits)

		// Stale version (2 < 3) is rejected
		require.ErrorIs(t, env.repo.ApplyRewardSet(ctx, errandID, uuid.New(), 10, 2, now), credit.ErrRewardSetStale)
	})

	t.Run("RefundReservation settles cancelled and expired errands atomically", func(t *testing.T) {
		requesterID := uuid.New()
		errandID := uuid.New()
		now := time.Now().UTC()

		require.NoError(t, env.repo.AllocateInitialCredits(ctx, requesterID, 100, now))
		_, _, err := env.repo.CreateReservation(ctx, errandID, requesterID, 30)
		require.NoError(t, err)

		// Settle refund
		eventID := uuid.New()
		err = env.repo.RefundReservation(ctx, errandID, nil, eventID, credit.TransactionReasonErrandCancelled, now)
		require.NoError(t, err)

		acc, err := env.repo.GetAccount(ctx, requesterID)
		require.NoError(t, err)
		require.Equal(t, int64(100), acc.AvailableCredits)
		require.Equal(t, int64(0), acc.ReservedCredits)

		var status credit.ReservationStatus
		require.NoError(t, env.pool.QueryRow(ctx, `SELECT status FROM credit_reservations WHERE errand_id = $1`, errandID).Scan(&status))
		require.Equal(t, credit.ReservationStatusRefunded, status)

		// Replaying same refund event is an idempotent no-op
		err = env.repo.RefundReservation(ctx, errandID, nil, eventID, credit.TransactionReasonErrandCancelled, now)
		require.NoError(t, err)

		// Payout on a refunded reservation returns a lifecycle violation
		courierID := uuid.New()
		amount := int64(30)
		err = env.repo.PayoutReservation(ctx, errandID, nil, courierID, uuid.New(), &amount, now)
		require.ErrorIs(t, err, credit.ErrLifecycleViolation)
	})

	t.Run("PayoutReservation performs 2-step atomic lazy wallet init and settlement", func(t *testing.T) {
		requesterID := uuid.New()
		courierID := uuid.New() // courier wallet NOT pre-created!
		errandID := uuid.New()
		now := time.Now().UTC()

		require.NoError(t, env.repo.AllocateInitialCredits(ctx, requesterID, 100, now))
		_, _, err := env.repo.CreateReservation(ctx, errandID, requesterID, 10)
		require.NoError(t, err)

		// Settle payout for 8 credits (with 2 credits surplus return to requester)
		amount := int64(8)
		eventID := uuid.New()
		err = env.repo.PayoutReservation(ctx, errandID, nil, courierID, eventID, &amount, now)
		require.NoError(t, err)

		// Requester balance: 90 available before payout + 2 surplus returned = 92 available, 0 reserved
		requester, err := env.repo.GetAccount(ctx, requesterID)
		require.NoError(t, err)
		require.Equal(t, int64(92), requester.AvailableCredits)
		require.Zero(t, requester.ReservedCredits)

		// Courier wallet was created on the fly with 100 starting credits + 8 earned = 108 available!
		courier, err := env.repo.GetAccount(ctx, courierID)
		require.NoError(t, err)
		require.Equal(t, int64(108), courier.AvailableCredits)
		require.Zero(t, courier.ReservedCredits)

		var status credit.ReservationStatus
		require.NoError(t, env.pool.QueryRow(ctx, `SELECT status FROM credit_reservations WHERE errand_id = $1`, errandID).Scan(&status))
		require.Equal(t, credit.ReservationStatusPaidOut, status)

		// When user.registered arrives later for this courier, it is an idempotent no-op
		err = env.repo.AllocateInitialCredits(ctx, courierID, 100, now)
		require.NoError(t, err)
		courierAfterReg, err := env.repo.GetAccount(ctx, courierID)
		require.NoError(t, err)
		require.Equal(t, int64(108), courierAfterReg.AvailableCredits)

		// Duplicate payout is an idempotent no-op
		require.NoError(t, env.repo.PayoutReservation(ctx, errandID, nil, courierID, eventID, &amount, now))

		// Ledger audit: exactly 1 registration allocation for courier + 1 payout + 1 deduction + 1 surplus return
		rows, err := env.pool.Query(ctx, `
			SELECT user_id, balance_bucket, amount, reason FROM credit_transactions
			WHERE operation_id = $1 ORDER BY balance_bucket, amount`, eventID)
		require.NoError(t, err)
		defer rows.Close()
		var ledger []struct {
			userID uuid.UUID
			bucket string
			amount int64
			reason credit.TransactionReason
		}
		for rows.Next() {
			var entry struct {
				userID uuid.UUID
				bucket string
				amount int64
				reason credit.TransactionReason
			}
			require.NoError(t, rows.Scan(&entry.userID, &entry.bucket, &entry.amount, &entry.reason))
			ledger = append(ledger, entry)
		}
		require.NoError(t, rows.Err())
		require.Len(t, ledger, 2)
		require.Equal(t, courierID, ledger[0].userID)
		require.Equal(t, int64(8), ledger[0].amount)
		require.Equal(t, credit.TransactionReasonErrandCompletedPayout, ledger[0].reason)
		require.Equal(t, requesterID, ledger[1].userID)
		require.Equal(t, int64(-8), ledger[1].amount)
		require.Equal(t, credit.TransactionReasonErrandCompletedDeduct, ledger[1].reason)

		leftoverOpID := uuid.NewSHA1(eventID, []byte("reward_decreased"))
		var leftoverCount int
		require.NoError(t, env.pool.QueryRow(ctx, `SELECT COUNT(*) FROM credit_transactions WHERE operation_id = $1`, leftoverOpID).Scan(&leftoverCount))
		require.Equal(t, 2, leftoverCount)

		flagged, err := env.repo.ReconcileAccount(ctx, requesterID)
		require.NoError(t, err)
		require.False(t, flagged)
	})

	t.Run("settlements reject mismatched requesterId as requester mismatch", func(t *testing.T) {
		requesterID, courierID, errandID := uuid.New(), uuid.New(), uuid.New()
		now := time.Now().UTC()
		require.NoError(t, env.repo.AllocateInitialCredits(ctx, requesterID, 100, now))
		_, _, err := env.repo.CreateReservation(ctx, errandID, requesterID, 20)
		require.NoError(t, err)

		wrongRequesterID := uuid.New()
		amount := int64(20)
		// Payout with wrong requesterId fails with ErrRequesterMismatch
		err = env.repo.PayoutReservation(ctx, errandID, &wrongRequesterID, courierID, uuid.New(), &amount, now)
		require.ErrorIs(t, err, credit.ErrRequesterMismatch)

		// Refund with wrong requesterId fails with ErrRequesterMismatch
		err = env.repo.RefundReservation(ctx, errandID, &wrongRequesterID, uuid.New(), credit.TransactionReasonErrandCancelled, now)
		require.ErrorIs(t, err, credit.ErrRequesterMismatch)

		// Refund with correct requesterId succeeds
		err = env.repo.RefundReservation(ctx, errandID, &requesterID, uuid.New(), credit.TransactionReasonErrandCancelled, now)
		require.NoError(t, err)
	})
}
