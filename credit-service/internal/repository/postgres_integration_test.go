//go:build integration

// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-20
// Scope: Integration tests for credit PostgresRepository covering migrations, initial allocation,
//   reservations, adjustments, D-057 errand.reward_set versioning, and idempotency over PostgreSQL 16.
// Author review: Reviewed by Seth

package repository_test

// AI-generated (edited by choppyback).
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

	t.Run("AllocateInitialCredits creates account and ledger rows", func(t *testing.T) {
		userID := uuid.New()
		now := time.Now().UTC()

		err := env.repo.AllocateInitialCredits(ctx, userID, 100, now)
		require.NoError(t, err)

		acc, err := env.repo.GetAccount(ctx, userID)
		require.NoError(t, err)
		require.Equal(t, int64(100), acc.AvailableCredits)
		require.Equal(t, int64(0), acc.ReservedCredits)

		// Duplicate allocation is idempotent no-op
		err = env.repo.AllocateInitialCredits(ctx, userID, 100, now)
		require.NoError(t, err)
	})

	t.Run("CreateReservation holds balance and returns created=true", func(t *testing.T) {
		userID := uuid.New()
		errandID := uuid.New()
		now := time.Now().UTC()

		require.NoError(t, env.repo.AllocateInitialCredits(ctx, userID, 100, now))

		res, created, err := env.repo.CreateReservation(ctx, errandID, userID, 30)
		require.NoError(t, err)
		require.True(t, created)
		require.Equal(t, int64(30), res.Amount)
		require.Equal(t, int64(1), res.Version)
		require.Equal(t, credit.ReservationStatusReserved, res.Status)

		acc, err := env.repo.GetAccount(ctx, userID)
		require.NoError(t, err)
		require.Equal(t, int64(70), acc.AvailableCredits)
		require.Equal(t, int64(30), acc.ReservedCredits)

		// Identical retry returns existing reservation with created=false
		res2, created2, err := env.repo.CreateReservation(ctx, errandID, userID, 30)
		require.NoError(t, err)
		require.False(t, created2)
		require.Equal(t, res.ID, res2.ID)

		// Mismatched retry returns conflict
		_, _, err = env.repo.CreateReservation(ctx, errandID, userID, 40)
		require.ErrorIs(t, err, credit.ErrReservationConflict)
	})

	t.Run("CreateReservation rejects insufficient credits", func(t *testing.T) {
		userID := uuid.New()
		errandID := uuid.New()
		now := time.Now().UTC()

		require.NoError(t, env.repo.AllocateInitialCredits(ctx, userID, 100, now))

		_, _, err := env.repo.CreateReservation(ctx, errandID, userID, 150)
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
		resOp, res, err := env.repo.AdjustReservationReward(ctx, errandID, userID, 60, op1)
		require.NoError(t, err)
		require.Equal(t, credit.OutcomeSuccess, resOp.OutcomeCode)
		require.Equal(t, int64(60), res.Amount)

		acc, err := env.repo.GetAccount(ctx, userID)
		require.NoError(t, err)
		require.Equal(t, int64(40), acc.AvailableCredits)
		require.Equal(t, int64(60), acc.ReservedCredits)

		// Repeated adjustment with same op1 is idempotent
		resOpRetry, resRetry, err := env.repo.AdjustReservationReward(ctx, errandID, userID, 60, op1)
		require.NoError(t, err)
		require.Equal(t, resOp.OperationID, resOpRetry.OperationID)
		require.Equal(t, res.Amount, resRetry.Amount)

		// 2. Decrease reward: 60 -> 25 (releases 35 credits back to available)
		op2 := uuid.New()
		resOp2, res2, err := env.repo.AdjustReservationReward(ctx, errandID, userID, 25, op2)
		require.NoError(t, err)
		require.Equal(t, credit.OutcomeSuccess, resOp2.OutcomeCode)
		require.Equal(t, int64(25), res2.Amount)

		acc2, err := env.repo.GetAccount(ctx, userID)
		require.NoError(t, err)
		require.Equal(t, int64(75), acc2.AvailableCredits)
		require.Equal(t, int64(25), acc2.ReservedCredits)

		// A later amendment may legitimately reuse an earlier target, but must use a new operation ID.
		op3 := uuid.New()
		resOp3, res3, err := env.repo.AdjustReservationReward(ctx, errandID, userID, 60, op3)
		require.NoError(t, err)
		require.Equal(t, credit.OutcomeSuccess, resOp3.OutcomeCode)
		require.Equal(t, int64(60), res3.Amount)
		acc3, err := env.repo.GetAccount(ctx, userID)
		require.NoError(t, err)
		require.Equal(t, int64(40), acc3.AvailableCredits)
		require.Equal(t, int64(60), acc3.ReservedCredits)

		// 3. Unowned reservation returns ErrReservationNotFound
		otherUser := uuid.New()
		op4 := uuid.New()
		_, _, err = env.repo.AdjustReservationReward(ctx, errandID, otherUser, 30, op4)
		require.ErrorIs(t, err, credit.ErrReservationNotFound)
	})

	t.Run("AdjustReservationReward commits a deterministic failure for retries", func(t *testing.T) {
		userID := uuid.New()
		errandID := uuid.New()
		require.NoError(t, env.repo.AllocateInitialCredits(ctx, userID, 100, time.Now().UTC()))
		_, _, err := env.repo.CreateReservation(ctx, errandID, userID, 90)
		require.NoError(t, err)

		opID := uuid.New()
		_, _, err = env.repo.AdjustReservationReward(ctx, errandID, userID, 110, opID)
		require.ErrorIs(t, err, credit.ErrInsufficientCredits)

		// Refund the reservation, which changes the wallet state. The same
		// operation ID must still replay its original outcome, not re-evaluate it.
		err = env.repo.RefundReservation(ctx, errandID, uuid.New(), credit.TransactionReasonErrandCancelled, time.Now().UTC())
		require.NoError(t, err)
		stored, _, err := env.repo.AdjustReservationReward(ctx, errandID, userID, 110, opID)
		require.ErrorIs(t, err, credit.ErrInsufficientCredits)
		require.NotNil(t, stored)
		require.Equal(t, credit.OutcomeInsufficientCredits, stored.OutcomeCode)
		var amount int64
		require.NoError(t, env.pool.QueryRow(ctx, `SELECT amount FROM credit_reservations WHERE errand_id = $1`, errandID).Scan(&amount))
		require.Equal(t, int64(90), amount)
	})

	t.Run("ApplyRewardSet (D-057) reconciles holds and enforces versioning", func(t *testing.T) {
		userID := uuid.New()
		errandID := uuid.New()
		now := time.Now().UTC()

		require.NoError(t, env.repo.AllocateInitialCredits(ctx, userID, 100, now))
		res, _, err := env.repo.CreateReservation(ctx, errandID, userID, 20)
		require.NoError(t, err)
		require.Equal(t, int64(1), res.Version)

		// AI-generated (edited by choppyback).
		// 1. The same version reconciles a PATCH whose response was lost without incrementing the version.
		eventRecovery := uuid.New()
		err = env.repo.ApplyRewardSet(ctx, errandID, eventRecovery, 50, 1, now)
		require.NoError(t, err)

		acc, err := env.repo.GetAccount(ctx, userID)
		require.NoError(t, err)
		require.Equal(t, int64(50), acc.AvailableCredits)
		require.Equal(t, int64(50), acc.ReservedCredits)
		var reservationAmount, reservationVersion int64
		require.NoError(t, env.pool.QueryRow(ctx, `
			SELECT amount, version
			FROM credit_reservations
			WHERE errand_id = $1`, errandID).Scan(&reservationAmount, &reservationVersion))
		require.Equal(t, int64(50), reservationAmount)
		require.Equal(t, int64(1), reservationVersion)

		// 2. Higher version (version: 2) updates hold to 35
		eventNew := uuid.New()
		err = env.repo.ApplyRewardSet(ctx, errandID, eventNew, 35, 2, now)
		require.NoError(t, err)

		acc, err = env.repo.GetAccount(ctx, userID)
		require.NoError(t, err)
		require.Equal(t, int64(65), acc.AvailableCredits)
		require.Equal(t, int64(35), acc.ReservedCredits)

		// 3. Exact re-delivery is harmless, while an older version is discarded.
		eventDuplicate := uuid.New()
		err = env.repo.ApplyRewardSet(ctx, errandID, eventDuplicate, 35, 2, now)
		require.NoError(t, err)
		eventOld := uuid.New()
		err = env.repo.ApplyRewardSet(ctx, errandID, eventOld, 50, 1, now)
		require.NoError(t, err)

		acc, err = env.repo.GetAccount(ctx, userID)
		require.NoError(t, err)
		require.Equal(t, int64(35), acc.ReservedCredits)

		// 4. Version 3 lowers hold to 10
		eventLower := uuid.New()
		err = env.repo.ApplyRewardSet(ctx, errandID, eventLower, 10, 3, now)
		require.NoError(t, err)

		acc, err = env.repo.GetAccount(ctx, userID)
		require.NoError(t, err)
		require.Equal(t, int64(90), acc.AvailableCredits)
		require.Equal(t, int64(10), acc.ReservedCredits)
	})

	t.Run("PayoutReservation (D-059) settles by event amount and returns leftover hold", func(t *testing.T) {
		requesterID := uuid.New()
		courierID := uuid.New()
		errandID := uuid.New()
		eventID := uuid.New()
		now := time.Now().UTC()

		require.NoError(t, env.repo.AllocateInitialCredits(ctx, requesterID, 100, now))
		require.NoError(t, env.repo.AllocateInitialCredits(ctx, courierID, 100, now))

		// Hold 50 credits
		_, _, err := env.repo.CreateReservation(ctx, errandID, requesterID, 50)
		require.NoError(t, err)

		// Event specifies amount 30 (leftover 20 returned to requester)
		settleAmount := int64(30)
		err = env.repo.PayoutReservation(ctx, errandID, courierID, eventID, &settleAmount, now)
		require.NoError(t, err)

		// Requester: started with 100, held 50 (available 50), returned 20 -> available 70, reserved 0
		reqAcc, err := env.repo.GetAccount(ctx, requesterID)
		require.NoError(t, err)
		require.Equal(t, int64(70), reqAcc.AvailableCredits)
		require.Equal(t, int64(0), reqAcc.ReservedCredits)

		// Courier: started with 100, received 30 -> available 130, reserved 0
		courierAcc, err := env.repo.GetAccount(ctx, courierID)
		require.NoError(t, err)
		require.Equal(t, int64(130), courierAcc.AvailableCredits)
		require.Equal(t, int64(0), courierAcc.ReservedCredits)
	})

	t.Run("pending payout retains completion amount through retry", func(t *testing.T) {
		requesterID, courierID, errandID, eventID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
		now := time.Now().UTC()
		require.NoError(t, env.repo.AllocateInitialCredits(ctx, requesterID, 100, now))
		_, _, err := env.repo.CreateReservation(ctx, errandID, requesterID, 10)
		require.NoError(t, err)

		amount := int64(8)
		require.NoError(t, env.repo.PayoutReservation(ctx, errandID, courierID, eventID, &amount, now))
		var pendingID uuid.UUID
		var storedAmount int64
		require.NoError(t, env.pool.QueryRow(ctx, `
			SELECT id, payout_amount FROM pending_payouts WHERE errand_id = $1`, errandID,
		).Scan(&pendingID, &storedAmount))
		require.Equal(t, amount, storedAmount)

		// The same event is harmless, but a changed amount cannot replace its durable intent.
		require.NoError(t, env.repo.PayoutReservation(ctx, errandID, courierID, eventID, &amount, now))
		conflictingAmount := int64(9)
		require.ErrorIs(t, env.repo.PayoutReservation(ctx, errandID, courierID, eventID, &conflictingAmount, now), credit.ErrLifecycleViolation)

		require.NoError(t, env.repo.AllocateInitialCredits(ctx, courierID, 100, now))
		// A redelivery after the blocker clears must still be compared with pending work.
		require.ErrorIs(t, env.repo.PayoutReservation(ctx, errandID, courierID, eventID, &conflictingAmount, now), credit.ErrLifecycleViolation)
		require.NoError(t, env.repo.ProcessPendingPayout(ctx, pendingID, time.Second))
		require.NoError(t, env.repo.ProcessPendingPayout(ctx, pendingID, time.Second))
		require.NoError(t, env.repo.PayoutReservation(ctx, errandID, courierID, eventID, &amount, now))

		requester, err := env.repo.GetAccount(ctx, requesterID)
		require.NoError(t, err)
		require.Equal(t, int64(92), requester.AvailableCredits)
		require.Zero(t, requester.ReservedCredits)
		courier, err := env.repo.GetAccount(ctx, courierID)
		require.NoError(t, err)
		require.Equal(t, int64(108), courier.AvailableCredits)

		var status credit.ReservationStatus
		require.NoError(t, env.pool.QueryRow(ctx, `SELECT status FROM credit_reservations WHERE errand_id = $1`, errandID).Scan(&status))
		require.Equal(t, credit.ReservationStatusPaidOut, status)
		var pendingCount int
		require.NoError(t, env.pool.QueryRow(ctx, `SELECT COUNT(*) FROM pending_payouts WHERE errand_id = $1`, errandID).Scan(&pendingCount))
		require.Zero(t, pendingCount)

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
		require.Len(t, ledger, 3)
		require.Equal(t, requesterID, ledger[0].userID)
		require.Equal(t, int64(2), ledger[0].amount)
		require.Equal(t, credit.TransactionReasonRewardDecreased, ledger[0].reason)
		require.Equal(t, courierID, ledger[1].userID)
		require.Equal(t, int64(8), ledger[1].amount)
		require.Equal(t, requesterID, ledger[2].userID)
		require.Equal(t, int64(-10), ledger[2].amount)
	})

	t.Run("pending payout uses completion amount for courier limit", func(t *testing.T) {
		requesterID, courierID, errandID, eventID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
		now := time.Now().UTC()
		require.NoError(t, env.repo.AllocateInitialCredits(ctx, requesterID, 100, now))
		require.NoError(t, env.repo.AllocateInitialCredits(ctx, courierID, 100, now))
		_, _, err := env.repo.CreateReservation(ctx, errandID, requesterID, 10)
		require.NoError(t, err)
		amount := int64(8)
		// Force the limit blocker, then make room for 8 credits but not the full hold of 10.
		_, err = env.pool.Exec(ctx, `UPDATE credit_accounts SET available_credits = $1 WHERE user_id = $2`, credit.MaxBalanceCredits-7, courierID)
		require.NoError(t, err)
		require.NoError(t, env.repo.PayoutReservation(ctx, errandID, courierID, eventID, &amount, now))
		var pendingID uuid.UUID
		var blockedReason string
		require.NoError(t, env.pool.QueryRow(ctx, `SELECT id, blocked_reason FROM pending_payouts WHERE errand_id = $1`, errandID).Scan(&pendingID, &blockedReason))
		require.Equal(t, "AVAILABLE_LIMIT_EXCEEDED", blockedReason)
		_, err = env.pool.Exec(ctx, `UPDATE credit_accounts SET available_credits = $1 WHERE user_id = $2`, credit.MaxBalanceCredits-8, courierID)
		require.NoError(t, err)
		require.NoError(t, env.repo.ProcessPendingPayout(ctx, pendingID, time.Second))
		courier, err := env.repo.GetAccount(ctx, courierID)
		require.NoError(t, err)
		require.Equal(t, credit.MaxBalanceCredits, courier.AvailableCredits)
		requester, err := env.repo.GetAccount(ctx, requesterID)
		require.NoError(t, err)
		require.Equal(t, int64(92), requester.AvailableCredits)
	})

	t.Run("pending payout survives flagged account", func(t *testing.T) {
		requesterID, courierID, errandID, eventID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
		now := time.Now().UTC()
		require.NoError(t, env.repo.AllocateInitialCredits(ctx, requesterID, 100, now))
		require.NoError(t, env.repo.AllocateInitialCredits(ctx, courierID, 100, now))
		_, _, err := env.repo.CreateReservation(ctx, errandID, requesterID, 10)
		require.NoError(t, err)
		_, err = env.pool.Exec(ctx, `
			UPDATE credit_accounts
			SET integrity_status = 'FLAGGED', integrity_flagged_at = NOW(), integrity_reason_code = 'TEST_FLAG'
			WHERE user_id = $1`, courierID)
		require.NoError(t, err)
		amount := int64(8)
		require.NoError(t, env.repo.PayoutReservation(ctx, errandID, courierID, eventID, &amount, now))
		var pendingID uuid.UUID
		var blockedReason string
		require.NoError(t, env.pool.QueryRow(ctx, `SELECT id, blocked_reason FROM pending_payouts WHERE errand_id = $1`, errandID).Scan(&pendingID, &blockedReason))
		require.Equal(t, "ACCOUNT_FLAGGED", blockedReason)
		_, err = env.pool.Exec(ctx, `
			UPDATE credit_accounts
			SET integrity_status = 'CONSISTENT', integrity_flagged_at = NULL, integrity_reason_code = NULL
			WHERE user_id = $1`, courierID)
		require.NoError(t, err)
		require.NoError(t, env.repo.ProcessPendingPayout(ctx, pendingID, time.Second))
		requester, err := env.repo.GetAccount(ctx, requesterID)
		require.NoError(t, err)
		require.Equal(t, int64(92), requester.AvailableCredits)
		courier, err := env.repo.GetAccount(ctx, courierID)
		require.NoError(t, err)
		require.Equal(t, int64(108), courier.AvailableCredits)
	})
}
