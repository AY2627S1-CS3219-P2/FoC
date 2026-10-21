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

		// 1. Stale version (1 <= 1) is ignored as a no-op
		eventOld := uuid.New()
		err = env.repo.ApplyRewardSet(ctx, errandID, eventOld, 50, 1, now)
		require.NoError(t, err)

		acc, err := env.repo.GetAccount(ctx, userID)
		require.NoError(t, err)
		require.Equal(t, int64(80), acc.AvailableCredits)
		require.Equal(t, int64(20), acc.ReservedCredits)

		// 2. Higher version (version: 2) updates hold to 35
		eventNew := uuid.New()
		err = env.repo.ApplyRewardSet(ctx, errandID, eventNew, 35, 2, now)
		require.NoError(t, err)

		acc, err = env.repo.GetAccount(ctx, userID)
		require.NoError(t, err)
		require.Equal(t, int64(65), acc.AvailableCredits)
		require.Equal(t, int64(35), acc.ReservedCredits)

		// 3. Re-delivery of version 2 or older version is discarded
		eventDuplicate := uuid.New()
		err = env.repo.ApplyRewardSet(ctx, errandID, eventDuplicate, 50, 2, now)
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
}
