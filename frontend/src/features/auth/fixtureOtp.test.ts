// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-20
// Scope: Unit tests for the OTP fixture's timing and rate-limit rules.
// Author review: Nigeltzy - AI was used to create the implementation for the unit tests for the OTP fixture and its functions. Checked and is valid and works as intended.

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { peek, resend, startRegistration, verify } from "./fixtureOtp";

const EMAIL = "nigel@u.nus.edu";
const USERNAME = "nigeltzy";

const FIVE_MIN = 5 * 60 * 1000;
const TEN_MIN = 10 * 60 * 1000;

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date("2026-09-20T10:00:00Z"));
});

afterEach(() => {
  vi.useRealTimers();
});

function start() {
  return startRegistration(EMAIL, USERNAME);
}

describe("startRegistration (F1.1.2.3)", () => {
  it("issues a six-digit code", () => {
    const pending = start();
    expect(pending.fixtureCode).toMatch(/^[0-9]{6}$/);
  });

  it("expires the code five minutes out (F1.1.2.5)", () => {
    const pending = start();
    expect(pending.expiresAt - Date.now()).toBe(FIVE_MIN);
  });

  it("starts with three replacements available (F1.1.2.6)", () => {
    expect(start().resendsRemaining).toBe(3);
  });
});

describe("verify (F1.1.2.7)", () => {
  it("completes registration on the correct code", () => {
    const pending = start();
    expect(verify(pending.handle, pending.fixtureCode!)).toEqual({
      username: USERNAME,
      email: EMAIL,
    });
  });

  it("rejects a wrong code", () => {
    const pending = start();
    const wrong = pending.fixtureCode === "000000" ? "111111" : "000000";
    expect(() => verify(pending.handle, wrong)).toThrow();
  });

  it("rejects a code that has expired", () => {
    const pending = start();
    vi.advanceTimersByTime(FIVE_MIN + 1000);
    expect(() => verify(pending.handle, pending.fixtureCode!)).toThrow(/expired/i);
  });

  it("accepts right up to the expiry boundary", () => {
    const pending = start();
    vi.advanceTimersByTime(FIVE_MIN - 1);
    expect(() => verify(pending.handle, pending.fixtureCode!)).not.toThrow();
  });

  it("consumes the registration, so a code works once", () => {
    const pending = start();
    verify(pending.handle, pending.fixtureCode!);
    expect(() => verify(pending.handle, pending.fixtureCode!)).toThrow();
  });

  it("rejects an unknown handle", () => {
    expect(() => verify("no-such-handle", "123456")).toThrow();
  });
});

describe("resend (F1.1.2.4, F1.1.2.5)", () => {
  it("invalidates the previous code", () => {
    const first = start();
    const oldCode = first.fixtureCode!;
    const second = resend(first.handle);
    // Guard against the fixture happening to generate the same digits.
    if (second.fixtureCode !== oldCode) {
      expect(() => verify(first.handle, oldCode)).toThrow();
    }
    expect(() => verify(first.handle, second.fixtureCode!)).not.toThrow();
  });

  it("gives the replacement a fresh five-minute window", () => {
    const first = start();
    vi.advanceTimersByTime(4 * 60 * 1000);
    const second = resend(first.handle);
    expect(second.expiresAt - Date.now()).toBe(FIVE_MIN);
  });

  it("decrements the allowance", () => {
    const pending = start();
    expect(resend(pending.handle).resendsRemaining).toBe(2);
    expect(resend(pending.handle).resendsRemaining).toBe(1);
    expect(resend(pending.handle).resendsRemaining).toBe(0);
  });
});

describe("resend rate limit (F1.1.2.6)", () => {
  it("allows three replacements then blocks the fourth", () => {
    const pending = start();
    resend(pending.handle);
    resend(pending.handle);
    resend(pending.handle);
    expect(() => resend(pending.handle)).toThrow(/too many/i);
  });

  it("keeps blocking for ten minutes", () => {
    const pending = start();
    resend(pending.handle);
    resend(pending.handle);
    resend(pending.handle);
    expect(() => resend(pending.handle)).toThrow();

    vi.advanceTimersByTime(TEN_MIN - 5000);
    expect(() => resend(pending.handle)).toThrow(/too many/i);
  });

  it("allows requests again once the block has passed", () => {
    const pending = start();
    resend(pending.handle);
    resend(pending.handle);
    resend(pending.handle);
    expect(() => resend(pending.handle)).toThrow();

    vi.advanceTimersByTime(TEN_MIN + 1000);
    expect(() => resend(pending.handle)).not.toThrow();
  });

  it("does not count replacements that fell outside the ten-minute window", () => {
    const pending = start();
    resend(pending.handle);
    resend(pending.handle);
    // Slide past the window so the first two no longer count.
    vi.advanceTimersByTime(TEN_MIN + 1000);
    resend(pending.handle);
    resend(pending.handle);
    expect(() => resend(pending.handle)).not.toThrow();
  });

  it("records the block so the UI can show a countdown", () => {
    const pending = start();
    resend(pending.handle);
    resend(pending.handle);
    resend(pending.handle);
    try {
      resend(pending.handle);
    } catch {
      // expected
    }
    const state = peek(pending.handle)!;
    expect(state.blockedUntil).not.toBeNull();
    expect(state.blockedUntil! - Date.now()).toBeLessThanOrEqual(TEN_MIN);
  });
});
