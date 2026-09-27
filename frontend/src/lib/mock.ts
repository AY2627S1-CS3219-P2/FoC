// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Shared helpers for the frontend-local mock services.
// Author review: Nigeltzy - AI built this for use when mock was needed, may not be used anymore depending on services implementation. Worked as intended previously. Deprecated.

/**
 * Shared helpers for the fixture modules that stand in for services the app
 * cannot call: order-service and credit-service, and user-service when
 * VITE_USE_FIXTURES=true. Each fixture lives in its feature's *Api.ts.
 *
 * The fixture shapes are not a contract. When a service's api/openapi.yaml
 * lands, its fixture module is replaced by that service's client, not
 * reconciled with it.
 */

/** Services that have a fixture module in this app. */
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
