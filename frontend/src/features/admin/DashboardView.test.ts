// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-03
// Scope: Tests for the dashboard's tab-bar keyboard rule.
// Author review: PENDING — <reviewer to complete>

import { describe, expect, it } from "vitest";
import { DASHBOARD_TABS, tabAfterKey } from "./DashboardView";

describe("tabAfterKey", () => {
  it("moves with the arrows and wraps at both ends", () => {
    expect(tabAfterKey("active", "ArrowRight")).toBe("past");
    expect(tabAfterKey("audit", "ArrowRight")).toBe("active");
    expect(tabAfterKey("active", "ArrowLeft")).toBe("audit");
    expect(tabAfterKey("accounts", "ArrowLeft")).toBe("past");
  });

  it("jumps to the ends with Home and End", () => {
    expect(tabAfterKey("accounts", "Home")).toBe(DASHBOARD_TABS[0]!.id);
    expect(tabAfterKey("accounts", "End")).toBe(DASHBOARD_TABS[DASHBOARD_TABS.length - 1]!.id);
  });

  it("ignores every other key", () => {
    expect(tabAfterKey("active", "Enter")).toBeNull();
    expect(tabAfterKey("active", "a")).toBeNull();
  });
});

// AI-generated (edited by Claude Code, 2026-11-09): the Flagged wallets tab sits
// between Accounts and Audit log (D-150).
describe("the Flagged wallets tab", () => {
  it("is between Accounts and Audit log for the arrow keys", () => {
    expect(DASHBOARD_TABS.map((t) => t.id)).toEqual(["active", "past", "accounts", "flagged", "audit"]);
    expect(tabAfterKey("accounts", "ArrowRight")).toBe("flagged");
    expect(tabAfterKey("flagged", "ArrowRight")).toBe("audit");
    expect(tabAfterKey("audit", "ArrowLeft")).toBe("flagged");
  });
});
