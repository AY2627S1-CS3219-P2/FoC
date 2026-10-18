// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-18
// Scope: Tests for statusControlFor, which picks the Admin screen's suspend or
//   reactivate control from a looked-up account's status, and for
//   formatDate, the looked-up account's Created date.
// Author review: James Teo — reviewed and verified.

import { describe, expect, it } from "vitest";
import { formatDate, statusControlFor } from "./AdminView";

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

  it("offers nothing when the profile carries no status", () => {
    expect(statusControlFor(undefined, false)).toBe("none");
  });
});
