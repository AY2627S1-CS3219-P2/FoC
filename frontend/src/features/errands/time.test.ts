// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-18
// Scope: Tests for the errand time helpers — the server clock offset, the
//   countdown and elapsed time read against it, the duration format at its
//   boundaries, and the date format.
// Author review: James Teo — reviewed and verified.

import { describe, expect, it } from "vitest";
import {
  clockOffset,
  countdown,
  elapsed,
  formatDateTime,
  formatDuration,
  msUntil,
  serverNow,
} from "./time";

const SERVER = Date.parse("2026-10-18T09:00:00Z");
const MINUTE = 60_000;

/** An ISO time `ms` after SERVER. */
function at(ms: number): string {
  return new Date(SERVER + ms).toISOString();
}

describe("clockOffset and serverNow", () => {
  it("measures how far the server's clock is ahead of the device's", () => {
    const deviceNow = SERVER - 90_000;
    expect(clockOffset(at(0), deviceNow)).toBe(90_000);
    expect(serverNow(90_000, deviceNow)).toBe(SERVER);
  });

  it("is negative when the server is behind", () => {
    expect(clockOffset(at(0), SERVER + 5_000)).toBe(-5_000);
  });

  it("falls back to the device clock when serverTime cannot be read", () => {
    expect(clockOffset("not a time", SERVER)).toBe(0);
  });
});

describe("countdown", () => {
  it("reads the deadline against the server's clock, not a wrong device clock", () => {
    // The device is 2 minutes slow; the errand expires 5 minutes after the
    // server's now. Without the offset it would read 7 minutes.
    const deviceNow = SERVER - 2 * MINUTE;
    const offset = clockOffset(at(0), deviceNow);
    expect(countdown(at(5 * MINUTE), serverNow(offset, deviceNow))).toEqual({
      due: false,
      text: "5 min",
    });
  });

  it("is due at the deadline and after it, never negative", () => {
    expect(countdown(at(0), SERVER)).toEqual({ due: true });
    expect(countdown(at(-30_000), SERVER)).toEqual({ due: true });
  });

  it("rounds the last second up, so it reads 1 s before it is due", () => {
    expect(countdown(at(400), SERVER)).toEqual({ due: false, text: "1 s" });
  });

  it("formats hours and minutes left", () => {
    expect(countdown(at(112 * MINUTE), SERVER)).toEqual({ due: false, text: "1 h 52 min" });
  });

  it("is null with no deadline or an unreadable one", () => {
    expect(countdown(undefined, SERVER)).toBeNull();
    expect(countdown("soon", SERVER)).toBeNull();
  });
});

describe("msUntil", () => {
  it("is the signed difference from now", () => {
    expect(msUntil(at(1_500), SERVER)).toBe(1_500);
    expect(msUntil(at(-1_500), SERVER)).toBe(-1_500);
  });

  it("is null with no time or an unreadable one", () => {
    expect(msUntil(undefined, SERVER)).toBeNull();
    expect(msUntil("", SERVER)).toBeNull();
    expect(msUntil("yesterday", SERVER)).toBeNull();
  });
});

describe("elapsed", () => {
  it("is how long ago a time was, rounded down", () => {
    expect(elapsed(at(-3 * 60 * MINUTE), SERVER)).toBe("3 h");
    expect(elapsed(at(-(4 * MINUTE + 59_000)), SERVER)).toBe("4 min");
  });

  it("reads a time slightly in the future as 0 s", () => {
    expect(elapsed(at(2_000), SERVER)).toBe("0 s");
  });

  it("is null with no time", () => {
    expect(elapsed(undefined, SERVER)).toBeNull();
  });
});

describe("formatDuration", () => {
  it.each([
    [0, "0 s"],
    [1, "1 s"],
    [45, "45 s"],
    [45.9, "45 s"],
    [59, "59 s"],
    [60, "1 min"],
    [119, "1 min"],
    [240, "4 min"],
    [3599, "59 min"],
    [3600, "1 h"],
    [3660, "1 h 1 min"],
    [6720, "1 h 52 min"],
    [82800, "23 h"],
    [-5, "0 s"],
  ])("formats %d seconds as %s", (seconds, text) => {
    expect(formatDuration(seconds)).toBe(text);
  });
});

describe("formatDateTime", () => {
  it("writes the day, short month and 24-hour time in the given zone", () => {
    expect(formatDateTime("2026-10-08T11:06:00Z", "Asia/Singapore")).toBe("8 Oct, 19:06");
    expect(formatDateTime("2026-10-08T16:30:00Z", "Asia/Singapore")).toBe("9 Oct, 00:30");
  });

  it("is empty for a time it cannot read", () => {
    expect(formatDateTime("not a time")).toBe("");
  });
});
