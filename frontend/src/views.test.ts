// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-18
// Scope: Tests for isViewAvailable: which views each session's nav lists, by
//   account role and, for a STUDENT, acting mode.
// Author review: James Teo — reviewed and verified.

import { describe, expect, it } from "vitest";
import type { AccountRole, ActingMode } from "./features/auth/types";
import { isViewAvailable, NAV_ITEMS, VIEW_NAMES, type ViewName } from "./views";

/** The views a session's nav lists, in nav order. */
function navFor(role: AccountRole, mode: ActingMode): ViewName[] {
  return NAV_ITEMS.filter((item) => isViewAvailable(item.view, role, mode)).map(
    (item) => item.view,
  );
}

describe("isViewAvailable", () => {
  it("lists a requesting student's views", () => {
    expect(navFor("STUDENT", "requesting")).toEqual([
      "home",
      "new-errand",
      "my-errands",
      "suppliers",
      "credits",
      "profile",
      "notifications",
    ]);
  });

  it("swaps New errand for Find errands in Delivering mode", () => {
    expect(navFor("STUDENT", "delivering")).toEqual([
      "home",
      "find-errands",
      "my-errands",
      "suppliers",
      "credits",
      "profile",
      "notifications",
    ]);
  });

  it("gives an admin All errands and Admin first, and no student screens, in either mode", () => {
    const expected: ViewName[] = [
      "home",
      "all-errands",
      "admin",
      "suppliers",
      "profile",
      "notifications",
    ];
    // An admin has no acting mode; whatever the state holds must not matter.
    expect(navFor("ADMIN", "requesting")).toEqual(expected);
    expect(navFor("ADMIN", "delivering")).toEqual(expected);
  });

  it("keeps the admin views from a student", () => {
    for (const mode of ["requesting", "delivering"] as const) {
      expect(isViewAvailable("admin", "STUDENT", mode)).toBe(false);
      expect(isViewAvailable("all-errands", "STUDENT", mode)).toBe(false);
    }
  });

  it("keeps the student views from an admin", () => {
    for (const view of ["new-errand", "find-errands", "my-errands", "credits"] as const) {
      expect(isViewAvailable(view, "ADMIN", "requesting")).toBe(false);
      expect(isViewAvailable(view, "ADMIN", "delivering")).toBe(false);
    }
  });

  it("offers each mode's own errand screen only in that mode", () => {
    expect(isViewAvailable("new-errand", "STUDENT", "delivering")).toBe(false);
    expect(isViewAvailable("find-errands", "STUDENT", "requesting")).toBe(false);
  });

  it("has a nav item for every view, so none is unreachable", () => {
    for (const view of VIEW_NAMES) {
      expect(NAV_ITEMS.some((item) => item.view === view)).toBe(true);
    }
  });

  it("refuses a name that is not a view", () => {
    expect(isViewAvailable("errands-v1" as ViewName, "STUDENT", "requesting")).toBe(false);
  });
});
