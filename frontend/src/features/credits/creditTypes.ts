// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-18
// Scope: Types for credit-service's account routes, hand-written by
//   transcribing credit-service/api/openapi.yaml: first from main at 27da39b,
//   then on 2026-10-21 from main at c914fe5 (#44): AccountResponse,
//   TransactionReason, TransactionOperation and the cursor-paged
//   TransactionListResponse. Replaces the mock's types.ts.
// Author review: James Teo — reviewed and verified.

/**
 * The shapes credit-service sends for the signed-in user's own account, field
 * for field as its api/openapi.yaml names them. Amounts are whole credits.
 * The reservation routes are order-service's to call, never the browser's, so
 * their shapes are not here.
 */

/** `GET /api/credits/accounts/me`, as the client returns it: only a READY account. */
export interface AccountResponse {
  userId: string;
  creditStatus: "READY";
  /** Spendable now. */
  availableCredits: number;
  /** Held on errands that are not final yet. */
  reservedCredits: number;
  /** RFC 3339. */
  updatedAt: string;
}

/** Why a transaction happened. */
export const TRANSACTION_REASONS = [
  "REGISTRATION_ALLOCATION",
  "ERRAND_RESERVATION",
  "ERRAND_FAILED",
  "ERRAND_COMPLETED_PAYOUT",
  "ERRAND_COMPLETED_DEDUCTION",
  "ERRAND_CANCELLED",
  "ERRAND_EXPIRED",
  "REWARD_INCREASED",
  "REWARD_DECREASED",
] as const;

export type TransactionReason = (typeof TRANSACTION_REASONS)[number];

/** One operation on the account: its change to each balance, signed as sent. */
export interface TransactionOperation {
  operationId: string;
  /** Absent or null for an operation no errand caused, such as the starting credits. */
  errandId?: string | null;
  reason: TransactionReason;
  availableDelta: number;
  reservedDelta: number;
  /** RFC 3339; absent or null when no event caused it. */
  occurredAt?: string | null;
  /** RFC 3339. */
  createdAt: string;
}

/** `GET /api/credits/accounts/me/transactions`: newest first. */
export interface TransactionListResponse {
  items: TransactionOperation[];
  /** Asks for the page after this one; null or absent on the last page. */
  nextCursor?: string | null;
}
