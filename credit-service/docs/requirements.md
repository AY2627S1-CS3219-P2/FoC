> Status: Owner-approved requirements for credit-service.
> Owner/reviewer: Seth
> Date: 2026-10-12

# Credit Service Requirements

## 1. Domain Overview & Invariants

### Entities & Data Model
- **CreditAccount**:
    - `userId`: String / UUID (Primary Key / Unique)
    - `availableCredits`: Integer (Default: 100)
    - `reservedCredits`: Integer (Default: 0)
    - `updatedAt`: Timestamp
- **CreditTransaction**:
    - `id`: String / UUID (Primary Key)
    - `userId`: String / UUID (Indexed)
    - `errandId`: String / UUID (Nullable, Indexed)
    - `amount`: Integer (Signed or absolute delta)
    - `reason`: String / Enum (`REGISTRATION_ALLOCATION`, `ERRAND_RESERVATION`, `ERRAND_COMPLETION_TRANSFER`, `ERRAND_CANCELLATION_RELEASE`, `ERRAND_REWARD_ADJUSTMENT`, etc.)
    - `createdAt`: Timestamp

### Core Invariants
1. **Non-negativity**: `availableCredits >= 0` and `reservedCredits >= 0` at all times.
2. **Auditability**: Every change to `availableCredits` or `reservedCredits` must produce an immutable `CreditTransaction` entry.
3. **Idempotency**: All notification consumers (e.g., Errand Completed, Errand Cancelled, Errand Expired) must be idempotent and safely handle duplicate deliveries using the unique `errandId`.
4. **Atomicity**: Multi-step balance updates (e.g., deducting from available and adding to reserved) must execute within a single ACID transaction.

---

## 2. Functional Requirements

| ID | Description | Priority |
| :--- | :--- | :--- |
| **F4.1** | Allocate 100 credits to each user upon successful registration. | High |
| **F4.1.1** | Create a credit balance for each newly registered user. | High |
| **F4.1.2** | Initialise the user's available credit balance to 100 credits. | High |
| **F4.1.3** | Record the initial credit allocation in the user's credit transaction history. | Medium |
| **F4.2** | Reserve required credits from the requester's available balance upon errand request creation. | High |
| **F4.2.1** | Reject the reservation when the requester has insufficient available credits. | High |
| **F4.2.2** | Deduct the reserved credit amount from the requester's available balance and transfer it to their reserved balance. | High |
| **F4.2.3** | Transfer back reserved credits to available balance upon receiving an errand creation failure notification. | High |
| **F4.3** | Transfer reserved credits to the assigned courier upon receiving errand completion notification. | High |
| **F4.3.1** | Deduct the errand's reserved amount from the requester's reserved-credit balance. | High |
| **F4.3.2** | Add the errand's reserved credit amount to the assigned courier's available-credit balance. | High |
| **F4.3.3** | Ensure idempotency: do not change any credit balance on duplicate completion notifications. | High |
| **F4.4** | Release reserved credits upon receiving a cancellation or expiry notification for an errand. | High |
| **F4.4.1** | Deduct reserved amount from requester's reserved balance and add back to available balance. | High |
| **F4.4.2** | Ensure idempotency: ignore duplicate or retried cancellation/expiry notifications. | High |
| **F4.5** | Adjust the requester's reserved credits when the reward for an open errand is changed. | Medium |
| **F4.5.1** | Reserve additional credits from available balance when the reward increases. | Medium |
| **F4.5.2** | Release the difference to available balance when the reward decreases. | Medium |
| **F4.5.3** | Reject reward modification if the requester has insufficient available credits for an increase. | Medium |
| **F4.6** | Record comprehensive audit trail for every credit balance modification. | Medium |
| **F4.6.1** | Record the reason for each balance change. | Medium |
| **F4.6.2** | Record the numerical delta of credits changed. | Medium |
| **F4.6.3** | Record the timestamp (date and time) of the balance change. | Medium |
| **F4.6.4** | Associate unique `errandId` for all errand-related transactions. | Medium |
| **F4.6.5** | Support non-errand balance updates with a `null` / optional `errandId`. | Medium |
| **F4.7** | Expose authenticated read APIs for user credit account and history. | Medium |
| **F4.7.1** | Return the user's available credit balance. | Medium |
| **F4.7.2** | Return the user's reserved credit balance. | Medium |
| **F4.7.3** | Return the paginated user credit transaction history. | Medium |

---

## 3. Detailed Specification

### F4.1 User Registration Credit Allocation
- **Trigger**: User registration event / callback.
- **Priority**: High
- **Rules**:
    - `F4.1.1`: Create `CreditAccount` row for `userId`.
    - `F4.1.2`: Set `availableCredits = 100`, `reservedCredits = 0`.
    - `F4.1.3`: Insert `CreditTransaction` (`amount = 100`, `reason = "INITIAL_REGISTRATION"`, `errandId = null`).

### F4.2 Errand Creation & Credit Reservation
- **Trigger**: Errand creation request or saga step.
- **Priority**: High
- **Rules**:
    - `F4.2.1`: Validate `availableCredits >= requiredCredits`. If false, throw `InsufficientCreditsException` and reject request.
    - `F4.2.2`: Atomic transfer:
        - `availableCredits -= requiredCredits`
        - `reservedCredits += requiredCredits`
        - Log `CreditTransaction` (`amount = -requiredCredits`, `reason = "ERRAND_RESERVATION"`, `errandId = <id>`).
    - `F4.2.3`: On errand creation rollback/failure notification:
        - `reservedCredits -= requiredCredits`
        - `availableCredits += requiredCredits`
        - Log compensation transaction.

### F4.3 Errand Completion & Payout
- **Trigger**: Errand completed notification.
- **Priority**: High
- **Rules**:
    - `F4.3.1`: Deduct reward amount from requester's `reservedCredits`.
    - `F4.3.2`: Credit reward amount to assigned courier's `availableCredits`.
    - `F4.3.3`: **Idempotency check**: Check if a completion transfer transaction already exists for this `errandId`. If so, acknowledge message and terminate without mutating balances.

### F4.4 Errand Cancellation & Expiry
- **Trigger**: Errand cancelled or expired notification.
- **Priority**: High
- **Rules**:
    - `F4.4.1`: Atomic refund:
        - `reservedCredits -= reservedAmount`
        - `availableCredits += reservedAmount`
        - Log `CreditTransaction` (`reason = "ERRAND_CANCELLATION" | "ERRAND_EXPIRY"`).
    - `F4.4.2`: **Idempotency check**: If credits for this `errandId` have already been released or transferred, ignore duplicate/retry events safely.

### F4.5 Errand Reward Adjustment
- **Trigger**: Requester edits reward on an open errand.
- **Priority**: Medium
- **Rules**:
    - Let `delta = newReward - oldReward`.
    - `F4.5.1` (If `delta > 0`):
        - Check if `requester.availableCredits >= delta`. If not, reject request (`F4.5.3`).
        - `requester.availableCredits -= delta`
        - `requester.reservedCredits += delta`
        - Log transaction (`amount = delta`, `reason = "REWARD_INCREASE"`).
    - `F4.5.2` (If `delta < 0`):
        - Let `releaseAmount = abs(delta)`.
        - `requester.reservedCredits -= releaseAmount`
        - `requester.availableCredits += releaseAmount`
        - Log transaction (`amount = releaseAmount`, `reason = "REWARD_DECREASE"`).

### F4.6 Transaction Logging & Auditability
- **Priority**: Medium
- **Rules**:
    - Every mutation must record:
        - `F4.6.1`: `reason` (Human-readable text or structured enum).
        - `F4.6.2`: `amount` (Credits altered).
        - `F4.6.3`: `createdAt` (UTC timestamp).
        - `F4.6.4`: `errandId` (UUID string when tied to an errand).
        - `F4.6.5`: `errandId = null` when operation is not errand-related (e.g., registration, admin grants).

### F4.7 Credit Query Interface
- **Priority**: Medium
- **Rules**:
    - Scope queries to authenticated `userId`.
    - `F4.7.1`: Expose endpoint/method returning `availableCredits`.
    - `F4.7.2`: Expose endpoint/method returning `reservedCredits`.
    - `F4.7.3`: Expose endpoint/method returning list of `CreditTransaction` records sorted by `createdAt DESC` (with pagination support).