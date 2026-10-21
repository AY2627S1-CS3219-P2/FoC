// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Shared helpers for the frontend-local mock services.
// Author review: Nigeltzy - AI built this for use when mock was needed, may not be used anymore depending on services implementation. Worked as intended previously. Deprecated.

// AI-generated (edited by James Teo): order-service and credit-service have
// real clients now and no fixture, so they are gone from this note and from
// MOCK_SERVICES, and nextErrandId went with the errand fixture.
/**
 * Shared helpers for the fixture modules that stand in for user-service and
 * supplier-service when VITE_USE_FIXTURES=true. Each fixture lives in its
 * feature folder. A fixture build has no order-service or credit-service
 * fixture: those screens show a notice instead.
 *
 * The fixture shapes are not a contract. When a service's api/openapi.yaml
 * lands, its fixture module is replaced by that service's client, not
 * reconciled with it.
 */

/** Services that have a fixture module in this app. */
export const MOCK_SERVICES = ["user", "supplier"] as const;

/**
 * Simulated latency, so loading and disabled states are exercised in
 * development rather than only appearing once a real network is involved.
 */
export function mockDelay<T>(value: T, ms = 320): Promise<T> {
  return new Promise((resolve) => setTimeout(() => resolve(value), ms));
}
