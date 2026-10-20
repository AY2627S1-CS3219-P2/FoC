// AI Assistance Disclosure:
// Tool: Antigravity, date: 2026-10-19
// Scope: Deterministic generated state-machine property unit test for credit service financial invariants per sourceOfTruth.md Phase 13.
// Author review: PENDING - reviewer to complete

package credit_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"foc/credit-service/internal/credit"

	"github.com/google/uuid"
)

// inMemoryStateMachineRepo implements repository ports for state-machine testing.
type inMemoryStateMachineRepo struct {
	mu               sync.Mutex
	accounts         map[uuid.UUID]*credit.Account
	reservations     map[uuid.UUID]*credit.Reservation
	transactions     []*credit.TransactionOperation
	operationResults map[uuid.UUID]*credit.OperationResult
	pendingPayouts   map[uuid.UUID]*credit.PendingPayout
	pendingRefunds   map[uuid.UUID]*credit.PendingRefund
}

func newInMemoryStateMachineRepo() *inMemoryStateMachineRepo {
	return &inMemoryStateMachineRepo{
		accounts:         make(map[uuid.UUID]*credit.Account),
		reservations:     make(map[uuid.UUID]*credit.Reservation),
		transactions:     make([]*credit.TransactionOperation, 0),
		operationResults: make(map[uuid.UUID]*credit.OperationResult),
		pendingPayouts:   make(map[uuid.UUID]*credit.PendingPayout),
		pendingRefunds:   make(map[uuid.UUID]*credit.PendingRefund),
	}
}

func (m *inMemoryStateMachineRepo) AllocateInitialCredits(_ context.Context, userID uuid.UUID, amount int64, occurredAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.accounts[userID]; exists {
		// Idempotent duplicate: no-op, commits without modifying balances
		return nil
	}

	acc := &credit.Account{
		UserID:           userID,
		AvailableCredits: 100,
		ReservedCredits:  0,
		IntegrityStatus:  credit.IntegrityConsistent,
		Version:          1,
		CreatedAt:        occurredAt,
		UpdatedAt:        occurredAt,
	}
	m.accounts[userID] = acc

	tx := &credit.TransactionOperation{
		OperationID:    uuid.New(),
		Reason:         credit.TransactionReasonRegistrationAllocation,
		AvailableDelta: 100,
		ReservedDelta:  0,
		OccurredAt:     &occurredAt,
		CreatedAt:      occurredAt,
	}
	// For replaying tests, associate with user via custom record or mapping
	m.recordUserTx(userID, tx)
	return nil
}

type userTx struct {
	userID uuid.UUID
	tx     *credit.TransactionOperation
}

type inMemoryLedger struct {
	history []userTx
}

var globalLedgers = make(map[*inMemoryStateMachineRepo]*inMemoryLedger)
var ledgerMu sync.Mutex

func (m *inMemoryStateMachineRepo) recordUserTx(userID uuid.UUID, tx *credit.TransactionOperation) {
	ledgerMu.Lock()
	defer ledgerMu.Unlock()
	l, ok := globalLedgers[m]
	if !ok {
		l = &inMemoryLedger{}
		globalLedgers[m] = l
	}
	l.history = append(l.history, userTx{userID: userID, tx: tx})
}

func (m *inMemoryStateMachineRepo) getUserHistory() []userTx {
	ledgerMu.Lock()
	defer ledgerMu.Unlock()
	l, ok := globalLedgers[m]
	if !ok {
		return nil
	}
	cp := make([]userTx, len(l.history))
	copy(cp, l.history)
	return cp
}

func (m *inMemoryStateMachineRepo) CreateReservation(_ context.Context, errandID, requesterID uuid.UUID, amount int64) (*credit.Reservation, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, exists := m.reservations[errandID]; exists {
		if existing.RequesterID == requesterID && existing.Amount == amount {
			return existing, false, nil
		}
		return nil, false, credit.ErrReservationConflict
	}

	acc, exists := m.accounts[requesterID]
	if !exists {
		return nil, false, credit.ErrWalletNotReady
	}
	if acc.IntegrityStatus != credit.IntegrityConsistent {
		return nil, false, credit.ErrAccountIntegrityFlagged
	}
	if acc.AvailableCredits < amount {
		return nil, false, credit.ErrInsufficientCredits
	}

	acc.AvailableCredits -= amount
	acc.ReservedCredits += amount
	acc.Version++
	now := time.Now().UTC()
	acc.UpdatedAt = now

	res := &credit.Reservation{
		ID:          uuid.New(),
		ErrandID:    errandID,
		RequesterID: requesterID,
		Amount:      amount,
		Status:      credit.ReservationStatusReserved,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	m.reservations[errandID] = res

	tx := &credit.TransactionOperation{
		OperationID:    uuid.New(),
		ErrandID:       &errandID,
		Reason:         credit.TransactionReasonErrandReservation,
		AvailableDelta: -amount,
		ReservedDelta:  amount,
		OccurredAt:     &now,
		CreatedAt:      now,
	}
	m.recordUserTx(requesterID, tx)

	return res, true, nil
}

func (m *inMemoryStateMachineRepo) AdjustReservationReward(_ context.Context, errandID uuid.UUID, newReward int64, operationID uuid.UUID) (*credit.OperationResult, *credit.Reservation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	if prev, exists := m.operationResults[operationID]; exists {
		res := m.reservations[errandID]
		if prev.OutcomeCode == credit.OutcomeInsufficientCredits {
			return prev, res, credit.ErrInsufficientCredits
		}
		return prev, res, nil
	}

	res, exists := m.reservations[errandID]
	if !exists {
		return nil, nil, credit.ErrReservationNotFound
	}
	if res.Status != credit.ReservationStatusReserved {
		return nil, nil, credit.ErrReservationStateConflict
	}

	currentAmount := res.Amount
	newAmount := newReward

	if currentAmount == newAmount {
		opRes := &credit.OperationResult{
			OperationID: operationID,
			ErrandID:    errandID,
			OutcomeCode: credit.OutcomeSuccess,
			CreatedAt:   now,
		}
		m.operationResults[operationID] = opRes
		return opRes, res, nil
	}

	acc := m.accounts[res.RequesterID]
	if acc == nil {
		return nil, nil, credit.ErrWalletNotReady
	}
	if acc.IntegrityStatus != credit.IntegrityConsistent {
		return nil, nil, credit.ErrAccountIntegrityFlagged
	}

	diff := newAmount - currentAmount
	if diff > 0 {
		// Reward increased: requester available debited, reserved credited
		if acc.AvailableCredits < diff {
			opRes := &credit.OperationResult{
				OperationID: operationID,
				ErrandID:    errandID,
				OutcomeCode: credit.OutcomeInsufficientCredits,
				CreatedAt:   now,
			}
			m.operationResults[operationID] = opRes
			return opRes, res, credit.ErrInsufficientCredits
		}
		acc.AvailableCredits -= diff
		acc.ReservedCredits += diff
		res.Amount = newAmount
		res.UpdatedAt = now

		tx := &credit.TransactionOperation{
			OperationID:    operationID,
			ErrandID:       &errandID,
			Reason:         credit.TransactionReasonRewardIncreased,
			AvailableDelta: -diff,
			ReservedDelta:  diff,
			OccurredAt:     &now,
			CreatedAt:      now,
		}
		m.recordUserTx(res.RequesterID, tx)
	} else {
		// Reward decreased: requester available credited, reserved debited
		dec := -diff
		acc.AvailableCredits += dec
		acc.ReservedCredits -= dec
		res.Amount = newAmount
		res.UpdatedAt = now

		tx := &credit.TransactionOperation{
			OperationID:    operationID,
			ErrandID:       &errandID,
			Reason:         credit.TransactionReasonRewardDecreased,
			AvailableDelta: dec,
			ReservedDelta:  -dec,
			OccurredAt:     &now,
			CreatedAt:      now,
		}
		m.recordUserTx(res.RequesterID, tx)
	}

	opRes := &credit.OperationResult{
		OperationID: operationID,
		ErrandID:    errandID,
		OutcomeCode: credit.OutcomeSuccess,
		CreatedAt:   now,
	}
	m.operationResults[operationID] = opRes
	return opRes, res, nil
}

func (m *inMemoryStateMachineRepo) RefundReservation(_ context.Context, errandID, eventID uuid.UUID, reason credit.TransactionReason, occurredAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	res, exists := m.reservations[errandID]
	if !exists {
		return credit.ErrReservationNotFound
	}
	if res.Status == credit.ReservationStatusRefunded {
		// duplicate redelivery
		return nil
	}
	if res.Status == credit.ReservationStatusPaidOut {
		return credit.ErrReservationStateConflict
	}

	acc := m.accounts[res.RequesterID]
	if acc == nil {
		return credit.ErrWalletNotReady
	}
	if acc.IntegrityStatus != credit.IntegrityConsistent {
		m.pendingRefunds[errandID] = &credit.PendingRefund{
			ID:          uuid.New(),
			ErrandID:    errandID,
			EventID:     eventID,
			Reason:      reason,
			FirstSeenAt: occurredAt,
		}
		return nil
	}

	acc.AvailableCredits += res.Amount
	acc.ReservedCredits -= res.Amount
	res.Status = credit.ReservationStatusRefunded
	now := time.Now().UTC()
	res.UpdatedAt = now

	tx := &credit.TransactionOperation{
		OperationID:    eventID,
		ErrandID:       &errandID,
		Reason:         reason,
		AvailableDelta: res.Amount,
		ReservedDelta:  -res.Amount,
		OccurredAt:     &occurredAt,
		CreatedAt:      now,
	}
	m.recordUserTx(res.RequesterID, tx)
	return nil
}

func (m *inMemoryStateMachineRepo) PayoutReservation(_ context.Context, errandID, courierID, eventID uuid.UUID, occurredAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	res, exists := m.reservations[errandID]
	if !exists {
		return credit.ErrReservationNotFound
	}
	if res.Status == credit.ReservationStatusPaidOut {
		// duplicate redelivery
		return nil
	}
	if res.Status == credit.ReservationStatusRefunded {
		return credit.ErrReservationStateConflict
	}
	if res.RequesterID == courierID {
		return credit.ErrSelfPayoutNotAllowed
	}

	courierAcc, courierExists := m.accounts[courierID]
	if !courierExists || courierAcc.IntegrityStatus != credit.IntegrityConsistent {
		m.pendingPayouts[errandID] = &credit.PendingPayout{
			ID:          uuid.New(),
			ErrandID:    errandID,
			EventID:     eventID,
			CourierID:   courierID,
			FirstSeenAt: occurredAt,
		}
		return nil
	}

	reqAcc := m.accounts[res.RequesterID]
	reqAcc.ReservedCredits -= res.Amount
	courierAcc.AvailableCredits += res.Amount
	res.Status = credit.ReservationStatusPaidOut
	now := time.Now().UTC()
	res.UpdatedAt = now

	tx1 := &credit.TransactionOperation{
		OperationID:    eventID,
		ErrandID:       &errandID,
		Reason:         credit.TransactionReasonErrandCompletedDeduct,
		AvailableDelta: 0,
		ReservedDelta:  -res.Amount,
		OccurredAt:     &occurredAt,
		CreatedAt:      now,
	}
	tx2 := &credit.TransactionOperation{
		OperationID:    eventID,
		ErrandID:       &errandID,
		Reason:         credit.TransactionReasonErrandCompletedPayout,
		AvailableDelta: res.Amount,
		ReservedDelta:  0,
		OccurredAt:     &occurredAt,
		CreatedAt:      now,
	}
	m.recordUserTx(res.RequesterID, tx1)
	m.recordUserTx(courierID, tx2)
	return nil
}

// verifyInvariants verifies all fundamental financial invariants across the repository.
func (m *inMemoryStateMachineRepo) verifyInvariants() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 1. Conservation of total credits:
	var totalAvailable, totalReserved int64
	for _, acc := range m.accounts {
		if acc.AvailableCredits < 0 || acc.AvailableCredits > 1_000_000_000 {
			return fmt.Errorf("account %s availableCredits %d out of bounds [0, 1000000000]", acc.UserID, acc.AvailableCredits)
		}
		if acc.ReservedCredits < 0 || acc.ReservedCredits > 1_000_000_000 {
			return fmt.Errorf("account %s reservedCredits %d out of bounds [0, 1000000000]", acc.UserID, acc.ReservedCredits)
		}
		totalAvailable += acc.AvailableCredits
		totalReserved += acc.ReservedCredits
	}

	expectedTotal := int64(len(m.accounts)) * 100
	actualTotal := totalAvailable + totalReserved
	if actualTotal != expectedTotal {
		return fmt.Errorf("credit conservation violation: total credits = %d, want %d (from %d accounts)", actualTotal, expectedTotal, len(m.accounts))
	}

	// 2. Replay ledger history per account:
	replayedAvailable := make(map[uuid.UUID]int64)
	replayedReserved := make(map[uuid.UUID]int64)
	for _, record := range m.getUserHistory() {
		replayedAvailable[record.userID] += record.tx.AvailableDelta
		replayedReserved[record.userID] += record.tx.ReservedDelta
	}

	for id, acc := range m.accounts {
		if replayedAvailable[id] != acc.AvailableCredits {
			return fmt.Errorf("account %s ledger replayed available %d != projected %d", id, replayedAvailable[id], acc.AvailableCredits)
		}
		if replayedReserved[id] != acc.ReservedCredits {
			return fmt.Errorf("account %s ledger replayed reserved %d != projected %d", id, replayedReserved[id], acc.ReservedCredits)
		}
	}

	// 3. Reservation status and active reservation sum check:
	activeReservedPerRequester := make(map[uuid.UUID]int64)
	for _, res := range m.reservations {
		switch res.Status {
		case credit.ReservationStatusReserved:
			activeReservedPerRequester[res.RequesterID] += res.Amount
		case credit.ReservationStatusPaidOut, credit.ReservationStatusRefunded:
			// terminal states
		default:
			return fmt.Errorf("invalid reservation status %s for errand %s", res.Status, res.ErrandID)
		}
	}

	for id, acc := range m.accounts {
		if activeReservedPerRequester[id] != acc.ReservedCredits {
			return fmt.Errorf("account %s sum of active reservations %d != reservedCredits %d", id, activeReservedPerRequester[id], acc.ReservedCredits)
		}
	}

	return nil
}

func TestCreditStateMachine_DeterministicGeneratedInvariants(t *testing.T) {
	totalSeeds := 200
	stepsPerSeed := 100

	if envSeeds := os.Getenv("STATE_MACHINE_SEEDS"); envSeeds != "" {
		if n, err := strconv.Atoi(envSeeds); err == nil && n > 0 {
			totalSeeds = n
		}
	}
	if envSteps := os.Getenv("STATE_MACHINE_STEPS"); envSteps != "" {
		if n, err := strconv.Atoi(envSteps); err == nil && n > 0 {
			stepsPerSeed = n
		}
	}

	for seed := int64(1); seed <= int64(totalSeeds); seed++ {
		r := rand.New(rand.NewSource(seed))
		repo := newInMemoryStateMachineRepo()
		svc := credit.NewService(
			repo,
			credit.WithReservationRepository(repo),
			credit.WithSettlementRepository(repo),
		)

		// Create 4 simulated users and 8 errands
		users := make([]uuid.UUID, 4)
		for i := range users {
			users[i] = uuid.New()
			_ = svc.AllocateInitialCredits(context.Background(), users[i], time.Now().UTC())
		}
		errands := make([]uuid.UUID, 8)
		for i := range errands {
			errands[i] = uuid.New()
		}

		opIDs := make([]uuid.UUID, 16)
		for i := range opIDs {
			opIDs[i] = uuid.New()
		}

		var historyLog []string

		for step := 1; step <= stepsPerSeed; step++ {
			action := r.Intn(6)
			ctx := context.Background()

			switch action {
			case 0: // Allocate initial credits (new user or duplicate)
				u := users[r.Intn(len(users))]
				if r.Float32() < 0.2 { // 20% brand new user
					u = uuid.New()
				}
				err := svc.AllocateInitialCredits(ctx, u, time.Now().UTC())
				historyLog = append(historyLog, fmt.Sprintf("step %d: AllocateInitialCredits(user=%s) -> %v", step, u, err))

			case 1: // Create reservation
				e := errands[r.Intn(len(errands))]
				u := users[r.Intn(len(users))]
				amt := int64(r.Intn(60) + 1) // 1 to 60 credits
				res, isNew, err := svc.CreateReservation(ctx, e, u, amt)
				historyLog = append(historyLog, fmt.Sprintf("step %d: CreateReservation(errand=%s, user=%s, amt=%d) -> res=%v, isNew=%v, err=%v", step, e, u, amt, res != nil, isNew, err))

			case 2: // Adjust reward
				e := errands[r.Intn(len(errands))]
				opID := opIDs[r.Intn(len(opIDs))]
				newAmt := int64(r.Intn(60) + 1)
				_, _, err := svc.AdjustReservationReward(ctx, e, newAmt, opID)
				historyLog = append(historyLog, fmt.Sprintf("step %d: AdjustReservationReward(errand=%s, next=%d, op=%s) -> err=%v", step, e, newAmt, opID, err))

			case 3: // Refund reservation
				e := errands[r.Intn(len(errands))]
				eventID := uuid.New()
				reasons := []credit.TransactionReason{credit.TransactionReasonErrandCancelled, credit.TransactionReasonErrandExpired, credit.TransactionReasonErrandFailed}
				reason := reasons[r.Intn(len(reasons))]
				err := svc.RefundReservation(ctx, e, eventID, reason, time.Now().UTC())
				historyLog = append(historyLog, fmt.Sprintf("step %d: RefundReservation(errand=%s, reason=%s) -> err=%v", step, e, reason, err))

			case 4: // Payout reservation
				e := errands[r.Intn(len(errands))]
				courier := users[r.Intn(len(users))]
				eventID := uuid.New()
				err := svc.PayoutReservation(ctx, e, courier, eventID, time.Now().UTC())
				historyLog = append(historyLog, fmt.Sprintf("step %d: PayoutReservation(errand=%s, courier=%s) -> err=%v", step, e, courier, err))

			case 5: // Replay / Duplicate settlement step
				e := errands[r.Intn(len(errands))]
				courier := users[r.Intn(len(users))]
				eventID := uuid.New()
				if r.Float32() < 0.5 {
					_ = svc.PayoutReservation(ctx, e, courier, eventID, time.Now().UTC())
					err := svc.PayoutReservation(ctx, e, courier, eventID, time.Now().UTC())
					historyLog = append(historyLog, fmt.Sprintf("step %d: Duplicate PayoutReservation(errand=%s) -> err=%v", step, e, err))
				} else {
					_ = svc.RefundReservation(ctx, e, eventID, credit.TransactionReasonErrandCancelled, time.Now().UTC())
					err := svc.RefundReservation(ctx, e, eventID, credit.TransactionReasonErrandCancelled, time.Now().UTC())
					historyLog = append(historyLog, fmt.Sprintf("step %d: Duplicate RefundReservation(errand=%s) -> err=%v", step, e, err))
				}
			}

			// Verify all invariants hold after every single step
			if err := repo.verifyInvariants(); err != nil {
				t.Fatalf("FAILED INVARIANT AT SEED %d STEP %d: %v\nExecution History:\n%s", seed, step, err, formatHistory(historyLog))
			}
		}
	}
}

func formatHistory(lines []string) string {
	start := 0
	if len(lines) > 20 {
		start = len(lines) - 20
	}
	out := ""
	for i := start; i < len(lines); i++ {
		out += fmt.Sprintf("  [%d] %s\n", i, lines[i])
	}
	return out
}

func TestCreditStateMachine_RejectsInvalidOperations(t *testing.T) {
	repo := newInMemoryStateMachineRepo()
	svc := credit.NewService(repo, credit.WithReservationRepository(repo), credit.WithSettlementRepository(repo))

	u1 := uuid.New()
	_ = svc.AllocateInitialCredits(context.Background(), u1, time.Now().UTC())

	t.Run("rejects self-payout", func(t *testing.T) {
		errandID := uuid.New()
		_, _, _ = svc.CreateReservation(context.Background(), errandID, u1, 50)
		err := svc.PayoutReservation(context.Background(), errandID, u1, uuid.New(), time.Now().UTC())
		if !errors.Is(err, credit.ErrSelfPayoutNotAllowed) {
			t.Fatalf("expected ErrSelfPayoutNotAllowed, got %v", err)
		}
	})

	t.Run("rejects post-terminal mutation", func(t *testing.T) {
		errandID := uuid.New()
		_, _, _ = svc.CreateReservation(context.Background(), errandID, u1, 30)
		_ = svc.RefundReservation(context.Background(), errandID, uuid.New(), credit.TransactionReasonErrandCancelled, time.Now().UTC())

		// Cannot adjust after terminal refund
		_, _, err := svc.AdjustReservationReward(context.Background(), errandID, 40, uuid.New())
		if !errors.Is(err, credit.ErrReservationStateConflict) {
			t.Fatalf("expected ErrReservationStateConflict, got %v", err)
		}
	})
}
