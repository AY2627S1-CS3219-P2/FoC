// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-03
// Scope: The types, parsers and pure helpers behind the admin dashboard's audit
//   log, account list and paging (D-104 to D-123): the one record shape the three
//   services share (D-107, D-113), turned from each service's JSON into one type,
//   merged by time, and described in words.
// Author review: PENDING — <reviewer to complete>

/** Rows a page holds, at most (D-104); the services cap their lists at it. */
export const PAGE_SIZE = 50;

/** One page of a list with the total that matched, for "page 3 of N". */
export interface Page<T> {
  items: T[];
  total: number;
  limit: number;
  offset: number;
}

/** Which service's audit log a row came from. */
export type AuditSource = "accounts" | "errands" | "suppliers" | "credits";

export const AUDIT_SOURCES: readonly AuditSource[] = ["accounts", "errands", "suppliers", "credits"];

export const AUDIT_SOURCE_LABEL: Record<AuditSource, string> = {
  accounts: "Accounts",
  errands: "Errands",
  suppliers: "Suppliers",
  credits: "Credits",
};

export type AuditOutcome = "succeeded" | "refused" | "failed";

export const AUDIT_OUTCOMES: readonly AuditOutcome[] = ["succeeded", "refused", "failed"];

/** One row of an admin audit log (D-107, D-113). The shape is the same in every service. */
export interface AuditEntry {
  id: string;
  source: AuditSource;
  /** RFC 3339, UTC. */
  occurredAt: string;
  actorId: string;
  actorRole: string;
  action: string;
  targetType: string;
  targetId: string;
  outcome: AuditOutcome;
  refusalReason: string;
  /** The admin's own reason (D-116); empty unless one is taken. */
  reason: string;
  beforeState: Record<string, unknown> | null;
  afterState: Record<string, unknown> | null;
  additionalComments: Record<string, unknown> | null;
  correlationId: string;
}

/** What an audit list can be narrowed by; every field is optional. */
export interface AuditFilter {
  action?: string;
  outcome?: AuditOutcome;
  actorId?: string;
  targetId?: string;
  /** YYYY-MM-DD or RFC 3339; inclusive. */
  from?: string;
  /** YYYY-MM-DD or RFC 3339; exclusive. */
  to?: string;
  limit?: number;
  offset?: number;
}

/** The audit actions the three services record (D-106), with their wording. */
export const AUDIT_ACTION_LABEL: Record<string, string> = {
  "account.suspend": "Suspended an account",
  "account.reactivate": "Reactivated an account",
  "account.password_reset": "Reset an account's password",
  "account.update": "Edited an account",
  "account.change_role": "Changed an account's role",
  "errand.cancel": "Cancelled an errand",
  "errand.complete": "Completed an errand",
  "supplier.create": "Created a supplier",
  "supplier.update": "Edited a supplier",
  "supplier.delete": "Deleted a supplier",
  "credit.reverify": "Re-verified a wallet",
};

export const AUDIT_ACTIONS: Record<AuditSource, readonly string[]> = {
  accounts: ["account.suspend", "account.reactivate", "account.password_reset", "account.update", "account.change_role"],
  errands: ["errand.cancel", "errand.complete"],
  suppliers: ["supplier.create", "supplier.update", "supplier.delete"],
  credits: ["credit.reverify"],
};

type Json = Record<string, unknown>;

function isRecord(value: unknown): value is Json {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function text(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function objectOrNull(value: unknown): Json | null {
  return isRecord(value) ? value : null;
}

function wholeNumber(value: unknown): number | null {
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

function outcomeOf(value: unknown): AuditOutcome | null {
  return AUDIT_OUTCOMES.includes(value as AuditOutcome) ? (value as AuditOutcome) : null;
}

/**
 * The page an audit endpoint answers with. user-service and supplier-service
 * write their fields in snake_case and order-service in camelCase; `camel`
 * says which. A body that is not the documented shape is null, which the client
 * reports as an unusable response rather than showing half of it.
 */
export function parseAuditPage(body: unknown, source: AuditSource, camel: boolean): Page<AuditEntry> | null {
  if (!isRecord(body) || !Array.isArray(body["items"])) return null;
  const total = wholeNumber(body["total"]);
  const limit = wholeNumber(body["limit"]);
  const offset = wholeNumber(body["offset"]);
  if (total === null || limit === null || offset === null) return null;
  const key = (snake: string, camelName: string) => (camel ? camelName : snake);
  const items: AuditEntry[] = [];
  for (const raw of body["items"]) {
    if (!isRecord(raw)) return null;
    const outcome = outcomeOf(raw["outcome"]);
    const occurredAt = text(raw[key("occurred_at", "occurredAt")]);
    if (!outcome || !occurredAt || !text(raw["id"])) return null;
    items.push({
      id: text(raw["id"]),
      source,
      occurredAt,
      actorId: text(raw[key("actor_id", "actorId")]),
      actorRole: text(raw[key("actor_role", "actorRole")]),
      action: text(raw["action"]),
      targetType: text(raw[key("target_type", "targetType")]),
      targetId: text(raw[key("target_id", "targetId")]),
      outcome,
      refusalReason: text(raw[key("refusal_reason", "refusalReason")]),
      reason: text(raw["reason"]),
      beforeState: objectOrNull(raw[key("before_state", "beforeState")]),
      afterState: objectOrNull(raw[key("after_state", "afterState")]),
      additionalComments: objectOrNull(raw[key("additional_comments", "additionalComments")]),
      correlationId: text(raw[key("correlation_id", "correlationId")]),
    });
  }
  return { items, total, limit, offset };
}

/**
 * Rows from several services' logs as one list, newest first. Ties keep the
 * order the sources were given in, so the result does not shuffle on a refresh.
 */
export function mergeAuditEntries(...lists: AuditEntry[][]): AuditEntry[] {
  const all = lists.flat();
  return all
    .map((entry, index) => ({ entry, index, time: Date.parse(entry.occurredAt) }))
    .sort((a, b) => {
      const byTime = (Number.isNaN(b.time) ? 0 : b.time) - (Number.isNaN(a.time) ? 0 : a.time);
      return byTime || a.index - b.index;
    })
    .map(({ entry }) => entry);
}

/** How many pages `total` rows make at `limit` a page; at least 1, so an empty list is "page 1 of 1". */
export function pageCount(total: number, limit: number): number {
  if (limit <= 0) return 1;
  return Math.max(1, Math.ceil(total / limit));
}

/** The 1-based page the offset falls on. */
export function pageNumber(offset: number, limit: number): number {
  if (limit <= 0) return 1;
  return Math.floor(offset / limit) + 1;
}

/** The offset of a 1-based page, never negative. */
export function offsetOfPage(page: number, limit: number): number {
  return Math.max(0, (page - 1) * limit);
}

/** One line of what an audit row changed, for the log. */
export interface AuditDetail {
  label: string;
  value: string;
}

/** A status or state change as "ACTIVE → SUSPENDED". Null when neither side is given. */
export function describeChange(entry: AuditEntry): string | null {
  const before = entry.beforeState ? Object.values(entry.beforeState)[0] : undefined;
  const after = entry.afterState ? Object.values(entry.afterState)[0] : undefined;
  if (before === undefined && after === undefined) return null;
  return `${String(before ?? "—")} → ${String(after ?? "—")}`;
}

/** The wording of the additional-comments keys that are not field names (D-127). */
const COMMENT_LABEL: Record<string, string> = {
  fields_changed: "Fields changed",
  fields_requested: "Fields requested",
};

/** A comment's value as text: a list of field names reads as "username, phone_num". */
function commentValue(value: unknown): string {
  if (typeof value === "string") return value;
  if (Array.isArray(value) && value.every((v) => typeof v === "string")) return value.join(", ");
  return JSON.stringify(value);
}

/**
 * What an audit row shows besides its time, actor, action and outcome: why it
 * was refused, the status or state it changed, the admin's reason, and what an
 * edit changed: a supplier's new values, or the names of an account's fields
 * (D-127). A password reset holds no value (D-111), so none is shown.
 */
export function auditDetails(entry: AuditEntry): AuditDetail[] {
  const details: AuditDetail[] = [];
  if (entry.outcome !== "succeeded" && entry.refusalReason) {
    details.push({ label: entry.outcome === "refused" ? "Refused because" : "Failed because", value: entry.refusalReason });
  }
  const change = describeChange(entry);
  if (change) details.push({ label: "Changed", value: change });
  if (entry.reason) details.push({ label: "Reason", value: entry.reason });
  if (entry.additionalComments) {
    for (const [field, value] of Object.entries(entry.additionalComments)) {
      details.push({ label: COMMENT_LABEL[field] ?? field, value: commentValue(value) });
    }
  }
  return details;
}

/** The wording of an action code, or the code itself for one this screen does not know. */
export function actionLabel(action: string): string {
  return AUDIT_ACTION_LABEL[action] ?? action;
}

/** The first 8 characters of an id, enough to tell rows apart on a phone. */
export function shortId(id: string): string {
  return id.length > 8 ? id.slice(0, 8) : id;
}

/**
 * Pages of several logs as one view of "page N of each". The rows are merged
 * newest first, the total is the sum, and the page count is the longest log's,
 * so paging forward reaches every row of every log.
 */
export function combinePages(pages: Page<AuditEntry>[]): { items: AuditEntry[]; total: number; pageCount: number } {
  return {
    items: mergeAuditEntries(...pages.map((p) => p.items)),
    total: pages.reduce((sum, p) => sum + p.total, 0),
    pageCount: Math.max(1, ...pages.map((p) => pageCount(p.total, p.limit))),
  };
}

/** The states an errand count can name, in the order a review lists them. */
const COUNT_ORDER = [
  "SCHEDULED",
  "OPEN",
  "ACCEPTED",
  "PICKED_UP",
  "DELIVERED",
  "COMPLETED",
  "CANCELLED",
  "EXPIRED",
] as const;

/**
 * A map of state to count as `[state, count]` pairs in lifecycle order, with
 * the zero and non-numeric entries dropped.
 */
export function countsSummary(counts: Partial<Record<string, number>>): [string, number][] {
  const known = COUNT_ORDER.filter((s) => (counts[s] ?? 0) > 0).map((s) => [s, counts[s] as number] as [string, number]);
  const others = Object.entries(counts).filter(
    ([state, n]) => !(COUNT_ORDER as readonly string[]).includes(state) && typeof n === "number" && n > 0,
  ) as [string, number][];
  return [...known, ...others];
}
