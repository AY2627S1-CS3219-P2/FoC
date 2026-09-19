// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Placeholder types for the mock credit-service.
// Author review: PENDING — <reviewer to complete>

/**
 * MOCK SHAPES — invented by the frontend, not a contract. `credit-service`
 * does not exist; how balances and the ledger are modelled is explicitly its
 * owner's decision (credit-service/AGENTS.md). See src/lib/mock.ts.
 *
 * Credits are whole numbers here on purpose: credit-service/AGENTS.md says to
 * flag any float representation of a credit amount, because rounding drift in
 * a ledger is silent and unrecoverable.
 */
export interface Balance {
  /** Spendable now. */
  available: number;
  /** Committed to errands that are still open. */
  held: number;
}

export type LedgerKind =
  | "allocation"
  | "reservation"
  | "payment"
  | "release";

export interface LedgerEntry {
  id: string;
  kind: LedgerKind;
  /** Human-readable description, as shown in the mockup. */
  label: string;
  /** Signed: negative moves credits out of available, positive returns them. */
  amount: number;
  /** Available balance immediately after this entry. */
  runningAvailable: number;
  /** ISO 8601. */
  occurredAt: string;
  /** Optional qualifier, e.g. "taken from held credits". */
  note?: string;
}
