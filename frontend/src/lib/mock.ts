// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Shared helpers for the frontend-local mock services.
// Author review: Nigeltzy - AI built this for use when mock was needed, may not be used anymore depending on services implementation. Worked as intended previously. Deprecated.

/**
 * ============================ READ THIS FIRST ============================
 *
 * `user-service`, `order-service` and `credit-service` DO NOT EXIST YET. To let
 * the UI be built and demoed, every feature whose backend is missing talks to a
 * fixture module in its own folder (`*Api.ts`, marked MOCK at the top) instead
 * of a real service.
 *
 * THE SHAPES IN THOSE FIXTURES ARE NOT A CONTRACT. They were invented by the
 * frontend to have something to render. Under root AGENTS.md §1 an API
 * interface is a design decision belonging to the service's owner, and §3 says
 * a change that reaches into another service needs that owner. So:
 *
 *   - Nobody may treat these shapes as the spec for their service.
 *   - When a real service lands, its owner's `api/openapi.yaml` wins, and the
 *     fixture module is REPLACED — not reconciled, not extended.
 *   - Because every component goes through the feature's api module and never
 *     touches these fixtures directly, that replacement is a one-file change.
 *
 * Any screen backed by one of these renders <MockBadge /> so a demo cannot read
 * invented numbers as live data.
 * =======================================================================
 */

/** True while any feature is still served by a fixture. */
export const MOCK_SERVICES = ["user", "order", "credit"] as const;

/**
 * Simulated latency, so loading and disabled states are exercised in
 * development rather than only appearing once a real network is involved.
 */
export function mockDelay<T>(value: T, ms = 320): Promise<T> {
  return new Promise((resolve) => setTimeout(() => resolve(value), ms));
}

/** Stable pseudo-id for fixtures, in the E-1039 style of the mockups. */
let sequence = 1040;
export function nextErrandId(): string {
  sequence += 1;
  return `E-${sequence}`;
}
