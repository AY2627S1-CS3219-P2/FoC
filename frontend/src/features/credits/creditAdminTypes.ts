// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-09
// Scope: Types, parsing and wording for credit-service's admin routes (D-150 to
//   D-153): the flagged accounts, an account's wallet as an admin sees it, and
//   what a Re-verify did. Transcribed from credit-service/api/openapi.yaml on
//   the same branch (FlaggedAccount, AdminAccountResponse, ReverifyResponse).
// Author review: PENDING — <reviewer to complete>

import type { Page } from "../admin/auditTypes";

/** A flagged account and the first discrepancy found on it (D-153). */
export interface FlaggedAccount {
  userId: string;
  reasonCode: string;
  detail?: string | null;
  /** Absent for a finding with one amount only, such as an out-of-range balance. */
  expected?: number | null;
  observed?: number | null;
  /** RFC 3339. */
  flaggedAt: string;
}

export type IntegrityStatus = "CONSISTENT" | "FLAGGED";

/**
 * An account as credit-service stores it. The balances are real even when the
 * account is flagged, but a flagged account's are not trusted (D-150).
 */
export interface AdminWallet {
  userId: string;
  availableCredits: number;
  reservedCredits: number;
  integrityStatus: IntegrityStatus;
  /** RFC 3339; the fields from here to `observed` are set only while flagged. */
  flaggedAt?: string | null;
  reasonCode?: string | null;
  detail?: string | null;
  expected?: number | null;
  observed?: number | null;
  createdAt: string;
  updatedAt: string;
}

/** What a Re-verify did (D-150). */
export type ReverifyOutcome = "CLEARED" | "STILL_FLAGGED" | "NOT_FLAGGED" | "STALE";

/**
 * The answer to a Re-verify. The reason, detail and amounts are the *current*
 * finding when the account is still flagged; credit-service stores none of
 * them (D-153).
 */
export interface ReverifyResult {
  outcome: ReverifyOutcome;
  reasonCode?: string | null;
  detail?: string | null;
  expected?: number | null;
  observed?: number | null;
}

const OUTCOMES: readonly string[] = ["CLEARED", "STILL_FLAGGED", "NOT_FLAGGED", "STALE"];

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function optionalText(value: unknown): boolean {
  return value === undefined || value === null || typeof value === "string";
}

function optionalNumber(value: unknown): boolean {
  return value === undefined || value === null || typeof value === "number";
}

function isFlaggedAccount(value: unknown): value is FlaggedAccount {
  return (
    isRecord(value) &&
    typeof value["userId"] === "string" &&
    typeof value["reasonCode"] === "string" &&
    typeof value["flaggedAt"] === "string" &&
    optionalText(value["detail"]) &&
    optionalNumber(value["expected"]) &&
    optionalNumber(value["observed"])
  );
}

/** The page the flagged-accounts route answers with; null when it is not the documented shape. */
export function parseFlaggedPage(body: unknown): Page<FlaggedAccount> | null {
  if (!isRecord(body) || !Array.isArray(body["items"])) return null;
  const { total, limit, offset } = body;
  if (typeof total !== "number" || typeof limit !== "number" || typeof offset !== "number") return null;
  if (!body["items"].every(isFlaggedAccount)) return null;
  return { items: body["items"] as FlaggedAccount[], total, limit, offset };
}

/** An account as the admin route sends it; null when it is not the documented shape. */
export function parseAdminWallet(body: unknown): AdminWallet | null {
  if (!isRecord(body)) return null;
  const status = body["integrityStatus"];
  const ok =
    typeof body["userId"] === "string" &&
    typeof body["availableCredits"] === "number" &&
    typeof body["reservedCredits"] === "number" &&
    (status === "CONSISTENT" || status === "FLAGGED") &&
    typeof body["createdAt"] === "string" &&
    typeof body["updatedAt"] === "string" &&
    optionalText(body["flaggedAt"]) &&
    optionalText(body["reasonCode"]) &&
    optionalText(body["detail"]) &&
    optionalNumber(body["expected"]) &&
    optionalNumber(body["observed"]);
  return ok ? (body as unknown as AdminWallet) : null;
}

/** A Re-verify's answer; null when it is not the documented shape. */
export function parseReverifyResult(body: unknown): ReverifyResult | null {
  if (!isRecord(body) || typeof body["outcome"] !== "string" || !OUTCOMES.includes(body["outcome"])) return null;
  const ok =
    optionalText(body["reasonCode"]) &&
    optionalText(body["detail"]) &&
    optionalNumber(body["expected"]) &&
    optionalNumber(body["observed"]);
  return ok ? (body as unknown as ReverifyResult) : null;
}

/** What each reason code means, in plain words. An unknown code is shown as sent. */
const REASON_LABEL: Record<string, string> = {
  OUT_OF_BOUNDS_BALANCE: "A balance is outside the allowed range",
  AVAILABLE_PROJECTION_MISMATCH: "The available balance doesn't match the ledger",
  RESERVED_PROJECTION_MISMATCH: "The held balance doesn't match the ledger or the reservations",
  RESERVATION_AMOUNT_MISMATCH: "A reservation's amount doesn't match its history",
  RESERVATION_LIFECYCLE_MISMATCH: "A reservation's settlement doesn't match its status",
  OPERATION_SHAPE_VIOLATION: "A ledger operation is malformed",
};

export function reasonLabel(code: string | null | undefined): string {
  if (!code) return "Unknown reason";
  return REASON_LABEL[code] ?? code;
}

function credits(n: number): string {
  return Number.isInteger(n) ? String(n) : String(Number(n.toFixed(2)));
}

/**
 * The two amounts a finding compares, as "Expected 100, observed 105". A finding
 * with one amount reads "Observed 105"; one with none is null.
 */
export function amountsText(expected?: number | null, observed?: number | null): string | null {
  const hasExpected = typeof expected === "number";
  const hasObserved = typeof observed === "number";
  if (hasExpected && hasObserved) return `Expected ${credits(expected)}, observed ${credits(observed)}`;
  if (hasObserved) return `Observed ${credits(observed)}`;
  if (hasExpected) return `Expected ${credits(expected)}`;
  return null;
}

/** How a screen reports a Re-verify: its kind picks the colour, its text is the sentence. */
export interface ReverifyMessage {
  kind: "success" | "info" | "error";
  text: string;
}

/**
 * The sentence for a Re-verify's outcome. Clearing a flag does not replay a
 * refund or payout the flag blocked: credit-service has already sent it to the
 * dead-letter queue, so the message says so (D-150).
 */
export function reverifyMessage(result: ReverifyResult, name: string): ReverifyMessage {
  switch (result.outcome) {
    case "CLEARED":
      return {
        kind: "success",
        text: `${name}'s wallet passed every check and is no longer flagged. A refund or payout the flag blocked is not replayed automatically.`,
      };
    case "STILL_FLAGGED": {
      const amounts = amountsText(result.expected, result.observed);
      return {
        kind: "error",
        text: `${name}'s wallet is still flagged: ${reasonLabel(result.reasonCode).toLowerCase()}${amounts ? ` (${amounts.toLowerCase()})` : ""}.`,
      };
    }
    case "NOT_FLAGGED":
      return { kind: "info", text: `${name}'s wallet isn't flagged, so there was nothing to clear.` };
    case "STALE":
      return { kind: "info", text: `${name}'s wallet changed while it was being checked. Try again.` };
  }
}
