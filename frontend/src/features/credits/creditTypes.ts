// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-18
// Scope: Types for credit-service's account routes, hand-written by
//   transcribing credit-service/api/openapi.yaml on main (commit 27da39b):
//   AccountResponse, TransactionReason, TransactionItem, Pagination and
//   TransactionListResponse. Replaces the mock's types.ts.
// Author review: James Teo — reviewed and verified.

/**
 * The shapes credit-service sends for the signed-in user's own account, field
 * for field as its api/openapi.yaml names them. Amounts are whole credits.
 * The reservation routes are order-service's to call, never the browser's, so
 * their shapes are not here.
 */

/** `GET /api/credits/accounts/me`. */
export interface AccountResponse {
  userId: string;
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
  "ERRAND_CREATION_FAILED",
  "ERRAND_COMPLETED_PAYOUT",
  "ERRAND_COMPLETED_DEDUCTION",
  "ERRAND_CANCELLED",
  "ERRAND_EXPIRED",
  "REWARD_INCREASED",
  "REWARD_DECREASED",
] as const;

export type TransactionReason = (typeof TRANSACTION_REASONS)[number];

export interface TransactionItem {
  id: string;
  userId: string;
  /** Null for a transaction no errand caused, such as the starting credits. */
  errandId: string | null;
  /**
   * Signed as credit-service sends it. The client shows it as it comes and
   * does no sign arithmetic of its own.
   */
  amount: number;
  reason: TransactionReason;
  /** RFC 3339. */
  createdAt: string;
}

export interface Pagination {
  /** 1–100; credit-service's default is 20. */
  limit: number;
  offset: number;
  /** Every transaction the account has, across all pages. */
  total: number;
}

/** `GET /api/credits/accounts/me/transactions`. */
export interface TransactionListResponse {
  items: TransactionItem[];
  pagination: Pagination;
}
