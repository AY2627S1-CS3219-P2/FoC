// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-03
// Scope: Tests for the audit log's parsing, merging, paging arithmetic and
//   wording (D-104 to D-123).
// Author review: PENDING — <reviewer to complete>

import { describe, expect, it } from "vitest";
import {
  actionLabel,
  AUDIT_ACTIONS,
  combinePages,
  countsSummary,
  shortId,
  auditDetails,
  describeChange,
  mergeAuditEntries,
  offsetOfPage,
  pageCount,
  pageNumber,
  parseAuditPage,
  type AuditEntry,
} from "./auditTypes";

const SNAKE_ROW = {
  id: "a1",
  occurred_at: "2026-11-02T10:00:00Z",
  actor_id: "admin-1",
  actor_role: "ADMIN",
  action: "account.suspend",
  target_type: "account",
  target_id: "u1",
  outcome: "succeeded",
  refusal_reason: "",
  reason: "",
  before_state: { account_status: "ACTIVE" },
  after_state: { account_status: "SUSPENDED" },
  additional_comments: null,
  correlation_id: "req-1",
};

const CAMEL_ROW = {
  id: "b1",
  occurredAt: "2026-11-02T11:00:00Z",
  actorId: "admin-1",
  actorRole: "ADMIN",
  action: "errand.cancel",
  targetType: "errand",
  targetId: "e1",
  outcome: "refused",
  refusalReason: "errand is already final",
  reason: "",
  correlationId: "req-2",
};

function entry(over: Partial<AuditEntry>): AuditEntry {
  return {
    id: "x",
    source: "accounts",
    occurredAt: "2026-11-02T10:00:00Z",
    actorId: "a",
    actorRole: "ADMIN",
    action: "account.suspend",
    targetType: "account",
    targetId: "t",
    outcome: "succeeded",
    refusalReason: "",
    reason: "",
    beforeState: null,
    afterState: null,
    additionalComments: null,
    correlationId: "",
    ...over,
  };
}

describe("parseAuditPage", () => {
  it("reads snake_case rows from user-service and supplier-service", () => {
    const page = parseAuditPage({ items: [SNAKE_ROW], total: 1, limit: 50, offset: 0 }, "accounts", false);
    expect(page?.items[0]).toMatchObject({
      id: "a1",
      source: "accounts",
      occurredAt: "2026-11-02T10:00:00Z",
      actorId: "admin-1",
      targetId: "u1",
      beforeState: { account_status: "ACTIVE" },
      correlationId: "req-1",
    });
    expect(page).toMatchObject({ total: 1, limit: 50, offset: 0 });
  });

  it("reads camelCase rows from order-service, with absent states as null", () => {
    const page = parseAuditPage({ items: [CAMEL_ROW], total: 1, limit: 50, offset: 0 }, "errands", true);
    expect(page?.items[0]).toMatchObject({
      outcome: "refused",
      refusalReason: "errand is already final",
      beforeState: null,
      targetType: "errand",
    });
  });

  it.each([
    ["not an object", "nope"],
    ["no items", { total: 0, limit: 50, offset: 0 }],
    ["no total", { items: [], limit: 50, offset: 0 }],
    ["an unknown outcome", { items: [{ ...SNAKE_ROW, outcome: "maybe" }], total: 1, limit: 50, offset: 0 }],
    ["a row with no time", { items: [{ ...SNAKE_ROW, occurred_at: "" }], total: 1, limit: 50, offset: 0 }],
    ["a row that is not an object", { items: [3], total: 1, limit: 50, offset: 0 }],
  ])("refuses %s", (_name, body) => {
    expect(parseAuditPage(body, "accounts", false)).toBeNull();
  });
});

describe("mergeAuditEntries", () => {
  it("puts the newest first across sources", () => {
    const a = entry({ id: "a", occurredAt: "2026-11-01T00:00:00Z" });
    const b = entry({ id: "b", occurredAt: "2026-11-03T00:00:00Z", source: "errands" });
    const c = entry({ id: "c", occurredAt: "2026-11-02T00:00:00Z", source: "suppliers" });
    expect(mergeAuditEntries([a], [b], [c]).map((e) => e.id)).toEqual(["b", "c", "a"]);
  });

  it("keeps the given order for rows at the same instant", () => {
    const a = entry({ id: "a" });
    const b = entry({ id: "b" });
    expect(mergeAuditEntries([a], [b]).map((e) => e.id)).toEqual(["a", "b"]);
  });

  it("is empty for no lists", () => {
    expect(mergeAuditEntries()).toEqual([]);
  });
});

describe("paging arithmetic", () => {
  it("counts at least one page", () => {
    expect(pageCount(0, 50)).toBe(1);
    expect(pageCount(50, 50)).toBe(1);
    expect(pageCount(51, 50)).toBe(2);
    expect(pageCount(10, 0)).toBe(1);
  });

  it("converts between offset and page", () => {
    expect(pageNumber(0, 50)).toBe(1);
    expect(pageNumber(100, 50)).toBe(3);
    expect(offsetOfPage(3, 50)).toBe(100);
    expect(offsetOfPage(0, 50)).toBe(0);
  });
});

describe("wording", () => {
  it("describes a state change", () => {
    const e = entry({ beforeState: { account_status: "ACTIVE" }, afterState: { account_status: "SUSPENDED" } });
    expect(describeChange(e)).toBe("ACTIVE → SUSPENDED");
    expect(describeChange(entry({}))).toBeNull();
  });

  it("lists why an attempt was refused, then the change, reason and edited fields", () => {
    const refused = entry({ outcome: "refused", refusalReason: "an admin account cannot be suspended" });
    expect(auditDetails(refused)).toEqual([
      { label: "Refused because", value: "an admin account cannot be suspended" },
    ]);

    const edit = entry({
      action: "supplier.update",
      reason: "typo",
      additionalComments: { name: "Deck", is_available: false },
    });
    expect(auditDetails(edit)).toEqual([
      { label: "Reason", value: "typo" },
      { label: "name", value: "Deck" },
      { label: "is_available", value: "false" },
    ]);
  });

  it("shows an account edit as the names of its fields, not values", () => {
    const edit = entry({ action: "account.update", additionalComments: { fields_changed: ["username", "phone_num"] } });
    expect(auditDetails(edit)).toEqual([{ label: "Fields changed", value: "username, phone_num" }]);
  });

  it("words a role change and shows the role it changed", () => {
    expect(actionLabel("account.change_role")).toBe("Changed an account's role");
    const change = entry({
      action: "account.change_role",
      beforeState: { account_role: "STUDENT" },
      afterState: { account_role: "ADMIN" },
    });
    expect(auditDetails(change)).toEqual([{ label: "Changed", value: "STUDENT → ADMIN" }]);
    expect(AUDIT_ACTIONS.accounts).toContain("account.change_role");
  });

  it("words a Re-verify and shows the status it changed", () => {
    expect(actionLabel("credit.reverify")).toBe("Re-verified a wallet");
    expect(AUDIT_ACTIONS.credits).toEqual(["credit.reverify"]);
    const cleared = entry({
      source: "credits",
      action: "credit.reverify",
      beforeState: { integrity_status: "FLAGGED" },
      afterState: { integrity_status: "CONSISTENT" },
      additionalComments: { reason_code: "AVAILABLE_PROJECTION_MISMATCH" },
    });
    expect(auditDetails(cleared)).toEqual([
      { label: "Changed", value: "FLAGGED → CONSISTENT" },
      { label: "reason_code", value: "AVAILABLE_PROJECTION_MISMATCH" },
    ]);
  });

  it("labels known actions and passes unknown ones through", () => {
    expect(actionLabel("errand.cancel")).toBe("Cancelled an errand");
    expect(actionLabel("something.new")).toBe("something.new");
  });
});

describe("combinePages", () => {
  it("merges rows, sums totals and takes the longest log's page count", () => {
    const a = entry({ id: "a", occurredAt: "2026-11-01T00:00:00Z" });
    const b = entry({ id: "b", occurredAt: "2026-11-02T00:00:00Z", source: "errands" });
    const combined = combinePages([
      { items: [a], total: 120, limit: 50, offset: 0 },
      { items: [b], total: 3, limit: 50, offset: 0 },
    ]);
    expect(combined.items.map((e) => e.id)).toEqual(["b", "a"]);
    expect(combined.total).toBe(123);
    expect(combined.pageCount).toBe(3);
  });

  it("is one empty page for no logs", () => {
    expect(combinePages([])).toEqual({ items: [], total: 0, pageCount: 1 });
  });
});

describe("countsSummary and shortId", () => {
  it("lists counts in lifecycle order and drops zeros", () => {
    expect(countsSummary({ COMPLETED: 3, OPEN: 1, CANCELLED: 0 })).toEqual([
      ["OPEN", 1],
      ["COMPLETED", 3],
    ]);
    expect(countsSummary({})).toEqual([]);
  });

  it("cuts an id to 8 characters", () => {
    expect(shortId("3f2b8c1e-1d2a-4b5c")).toBe("3f2b8c1e");
    expect(shortId("abc")).toBe("abc");
  });
});
