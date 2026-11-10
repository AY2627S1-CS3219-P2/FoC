// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-18
// Scope: Tests for statusControlFor, which picks the Admin screen's suspend or
//   reactivate control from a looked-up account's status, and for
//   formatDate, the looked-up account's Created date.
// Author review: James Teo — reviewed and verified.

import { describe, expect, it } from "vitest";
import { formatDate, roleControlFor, statusControlFor } from "./AdminView";

describe("formatDate", () => {
  it("shows a date and time as the Credits screen does, not the US locale", () => {
    expect(formatDate("2026-10-18T12:37:55Z", "Asia/Singapore")).toBe("18 Oct, 20:37");
    expect(formatDate("2026-10-08T16:30:00Z", "Asia/Singapore")).toBe("9 Oct, 00:30");
  });

  it("leaves out a missing date", () => {
    expect(formatDate(undefined)).toBeUndefined();
    expect(formatDate("")).toBeUndefined();
  });

  it("shows an unreadable date as sent", () => {
    expect(formatDate("not a date")).toBe("not a date");
  });
});

describe("statusControlFor", () => {
  it.each([
    ["ACTIVE", "suspend"],
    ["SUSPENDED", "reactivate"],
    ["UNVERIFIED", "not-verified"],
  ] as const)("offers another user's %s account %s", (status, control) => {
    expect(statusControlFor(status, false)).toBe(control);
  });

  it.each(["ACTIVE", "SUSPENDED", "UNVERIFIED"] as const)(
    "offers nothing on your own %s account",
    (status) => {
      expect(statusControlFor(status, true)).toBe("none");
    },
  );

  it("offers no suspend on an active admin account, but can still reactivate a suspended one", () => {
    expect(statusControlFor("ACTIVE", false, "ADMIN")).toBe("none");
    expect(statusControlFor("ACTIVE", false, "STUDENT")).toBe("suspend");
    expect(statusControlFor("SUSPENDED", false, "ADMIN")).toBe("reactivate");
  });

  it("offers nothing when the profile carries no status", () => {
    expect(statusControlFor(undefined, false)).toBe("none");
  });
});

// AI-generated (edited by Claude Code, 2026-11-09): the role control (D-149).
describe("roleControlFor", () => {
  it("offers Make admin on an active student only", () => {
    expect(roleControlFor("ACTIVE", "STUDENT", false)).toBe("promote");
    expect(roleControlFor("SUSPENDED", "STUDENT", false)).toBe("none");
    expect(roleControlFor("UNVERIFIED", "STUDENT", false)).toBe("none");
  });

  it("offers Remove admin on another admin, active or not", () => {
    expect(roleControlFor("ACTIVE", "ADMIN", false)).toBe("demote");
    expect(roleControlFor("SUSPENDED", "ADMIN", false)).toBe("demote");
  });

  it("offers nothing on your own account", () => {
    expect(roleControlFor("ACTIVE", "ADMIN", true)).toBe("none");
    expect(roleControlFor("ACTIVE", "STUDENT", true)).toBe("none");
  });

  it("offers nothing when the profile carries no role", () => {
    expect(roleControlFor("ACTIVE", undefined, false)).toBe("none");
  });
});
