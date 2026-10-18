# order-service requirements

The requirements order-service and its frontend screens are built against, as
the team wrote them. Decisions on how each one is met are the D-0xx rows in
[`ai/decisions.md`](../../ai/decisions.md).

## 1. Order lifecycle

- **F3:** The Order Service shall manage the complete lifecycle of an errand request.
- **F3.1:** Allow an authorised user to create an errand request.
- **F3.1.1:** Record a unique request identifier, requester’s account identifier, pickup supplier, item description, delivery location, allocated credit reward, current order state, and creation time.
- **F3.1.2:** Validate the credit reward (must be a positive integer).
- **F3.1.3:** Validate the pickup supplier (must reference a valid, existing supplier record).
- **F3.1.4:** Validate the delivery location (must be within the NUS campus).
- **F3.1.5:** Reserve the requester’s specified credit reward through the Credit Service before creating the errand.
- **F3.1.6:** Ensure no errand record is created if creation fails after credit reservation, and notify the Credit Service to cancel the reservation.
- **F3.1.7:** Create the request in the OPEN state.
- **F3.2:** Provide potential couriers with currently open errand requests.
- **F3.2.1:** Contain only requests in the OPEN state in the open-request listing.
- **F3.2.2:** Return the listing sorted by creation time (most recent first) unless filtered otherwise.
- **F3.2.3:** Provide pickup supplier, delivery location, credit reward, and time remaining before expiry for each request.
- **F3.3:** Allow a user to accept an open errand request.
- **F3.3.1:** Reject acceptance made by the requester who created the request.
- **F3.3.2:** Assign at most one courier to each errand.
- **F3.3.3:** Process concurrent acceptance attempts so exactly one valid acceptance changes the errand to ACCEPTED, while others are rejected.
- **F3.3.4:** Reject acceptance by a courier who already holds $K$ active assignments that have not reached DELIVERED.
- **F3.4:** Allow a requester to track the errands they have created.
- **F3.4.1:** List non-terminal errands created by the requester with their current states.
- **F3.4.2:** Indicate for DELIVERED errands that confirmation is outstanding and show time remaining before automatic confirmation.
- **F3.4.3:** List completed, cancelled, and expired errands as order history.
- **F3.5:** Allow a courier to track the errands they have accepted.
- **F3.5.1:** List non-terminal errands assigned to the courier with their current states.
- **F3.5.2:** Show pickup supplier, item description, delivery location, and credit reward for each assigned errand.
- **F3.5.3:** List completed, cancelled, or released assignments as assignment history.
- **F3.6:** Display the full detail of a single errand.
- **F3.6.1:** Show pickup supplier, item description, delivery location, credit reward, current state, state transition times, and assigned courier.
- **F3.6.2:** Accessible by requester, assigned courier, any authenticated user while OPEN, and ADMIN users.
- **F3.6.3:** Reject requests for errand details from unauthorized users.
- **F3.7:** Allow the requester to amend an errand while it is OPEN.
- **F3.7.1:** Allow pickup supplier, item description, delivery location, and credit reward to be amended.
- **F3.7.2:** Subject amendments to the same validation as creation.
- **F3.7.3:** Reject amendments by any user other than the creating requester.
- **F3.7.4:** Reject amendments to errands that are no longer OPEN.
- **F3.7.5:** Request the Credit Service to adjust reserved credits on reward changes.
- **F3.7.6:** Do not reset the expiry deadline upon successful amendment.
- **F3.7.7:** Record amendment timestamps and changed fields.
- **F3.8:** Allow the assigned courier to mark the request as picked up.
- **F3.8.1:** Reject pickup markings by any user other than the assigned courier.
- **F3.9:** Allow the assigned courier to mark the errand as delivered.
- **F3.9.1:** Reject delivery markings by any user other than the assigned courier.
- **F3.9.2:** Allow only the creating requester to confirm delivery when in the DELIVERED state.
- **F3.9.3:** Move the request to COMPLETED upon valid delivery confirmation.
- **F3.9.4:** Automatically confirm DELIVERED requests after confirmation window $C$ elapses.
- **F3.10:** Handle errand expiry and courier withdrawal.
- **F3.10.1:** Expire OPEN requests once expiry period $T$ elapses.
- **F3.10.2:** Reject updates to expired errands.
- **F3.10.3:** Permit ADMIN users to make updates/deletions to any errand in any state.
- **F3.11:** Allow the cancellation of errands.
- **F3.11.1:** Allow the requester to cancel while OPEN; reject once ACCEPTED.
- **F3.11.2:** Soft-delete an OPEN request when cancelled.
- **F3.12:** Allow courier withdrawal.
- **F3.12.1:** Allow the assigned courier to withdraw while in the ACCEPTED state.
- **F3.12.2:** Reject withdrawal after reaching the PICKED_UP state.
- **F3.12.3:** Remove courier assignment and return the errand to the OPEN state upon successful withdrawal.
- **F3.13:** Enforce the defined order state machine.
- **F3.13.1:** Record the time of every state transition.
- **F3.13.2:** Reject unpermitted state transitions.
- **F3.14:** Schedule asynchronous expiry handling when an errand enters the OPEN state.
- **F3.14.1:** Verify the errand remains OPEN before expiring it.

## 2. Frontend and interface (Section 2.7)

- **F7:** Support complete requester and courier workflows through a responsive interface.
- **F7.1:** Report the outcome of every user action (explicit error messages, state restoration on failure, pending state indicators).
- **F7.2:** Protect against duplicate submissions (disable controls while in flight, attach client request IDs).
- **F7.3:** Present helpful empty states for open-request and my-errands listings with primary actions.
- **F7.4.2:** Require explicit confirmation before cancelling an errand (stating credit release consequences and irreversibility).
- **F7.5:** Keep both requester and courier current on errand state changes without manual reloads.

## 3. Related non-functional requirements

- **NFR1:** Responsive and intuitive interface for requester and courier workflows across viewports (360px, 768px, 1440px).
- **NFR2.3:** Write path (accept-request operation) must complete within 1.0s at p95 and 2.0s at p99.
- **NFR4.4:** Credit-affecting operations must be authorized against both the user's role and their relationship to the errand (requester or courier).
- **NFR5.1:** Restarting stateless services shall not lose committed order or credit state.
- **NFR5.2:** Order lifecycle events consumed by dependent services must reflect in state within 2s at p95 and 5s at p99.
- **NFR5.4:** Order state changes committed during message broker outages must reflect in dependent services within 5 minutes of recovery.
- **NFR5.5:** Credit Service must deduct reserved credits and transfer them to the courier's balance atomically.
- **NFR7.1:** State-changing API requests and asynchronous order lifecycle events must carry a correlation identifier in structured logs.
