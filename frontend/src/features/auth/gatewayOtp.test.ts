// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-10
// Scope: Tests for the gateway client's OTP calls — register, verify, resend
//   and resuming an unverified account — against a stubbed fetch, plus the
//   unverified-login error.
// Author review: PENDING

import { afterEach, describe, expect, it, vi } from "vitest";

import {
  AuthError,
  logInViaGateway,
  OtpBlockedError,
  resendOtpViaGateway,
  resumeVerificationViaGateway,
  signUpViaGateway,
  UnverifiedAccountError,
  verifyRegistrationViaGateway,
} from "./authApi";
import type { PendingRegistration } from "./types";

/** These tests stub `fetch`, as gatewayRefreshLogout.test.ts does. */

function jsonResponse(status: number, body: unknown) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: () => Promise.resolve(body),
  } as unknown as Response;
}

function stubFetch(...responses: Response[]) {
  const fetchMock = vi.fn();
  for (const response of responses) fetchMock.mockResolvedValueOnce(response);
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

/** The URL and parsed JSON body of the fetch call at `index`. */
function request(fetchMock: ReturnType<typeof vi.fn>, index = 0) {
  const [url, init] = fetchMock.mock.calls[index] as [string, RequestInit];
  return { url, method: init.method, body: JSON.parse(String(init.body)) };
}

const EMAIL = "student@u.nus.edu";
const FIVE_MINUTES = 5 * 60 * 1000;
const TEN_MINUTES = 10 * 60 * 1000;

function pending(resendsRemaining: number | null): PendingRegistration {
  return {
    handle: EMAIL,
    email: EMAIL,
    expiresAt: Date.now() + FIVE_MINUTES,
    resendsRemaining,
    blockedUntil: null,
  };
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe("signUpViaGateway", () => {
  it("posts to /auth/register and opens a pending registration", async () => {
    vi.useFakeTimers({ now: 1_000_000 });
    const fetchMock = stubFetch(jsonResponse(201, null));

    const result = await signUpViaGateway(` ${EMAIL} `, "student", "Passw0rd", "91234567");

    const { url, method, body } = request(fetchMock);
    expect(url).toMatch(/\/auth\/register$/);
    expect(method).toBe("POST");
    // No contact: user-service's register takes no phone number.
    expect(body).toEqual({ email: EMAIL, username: "student", password: "Passw0rd" });
    expect(result).toEqual({
      handle: EMAIL,
      email: EMAIL,
      expiresAt: 1_000_000 + FIVE_MINUTES,
      // user-service counts the first code, so two replacements are left.
      resendsRemaining: 2,
      blockedUntil: null,
    });
  });

  it("sends nothing when the form is invalid", async () => {
    const fetchMock = stubFetch();

    await expect(
      signUpViaGateway("someone@gmail.com", "student", "Passw0rd", ""),
    ).rejects.toBeInstanceOf(AuthError);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it.each([
    [409, /already taken/],
    [429, /too many code requests/i],
    [500, /could not send your code/i],
  ])("names a %i outcome", async (status, message) => {
    stubFetch(jsonResponse(status, { error: "whatever" }));

    await expect(
      signUpViaGateway(EMAIL, "student", "Passw0rd", ""),
    ).rejects.toThrow(message);
  });
});

describe("verifyRegistrationViaGateway", () => {
  it("posts the email and code to /auth/verify-otp", async () => {
    const fetchMock = stubFetch(jsonResponse(200, {}));

    await expect(
      verifyRegistrationViaGateway(pending(2), " 123456 "),
    ).resolves.toBeUndefined();

    const { url, body } = request(fetchMock);
    expect(url).toMatch(/\/auth\/verify-otp$/);
    expect(body).toEqual({ email: EMAIL, otp: "123456" });
  });

  it("treats an already-verified account as success", async () => {
    stubFetch(jsonResponse(409, { error: "Account is already verified" }));

    await expect(
      verifyRegistrationViaGateway(pending(2), "123456"),
    ).resolves.toBeUndefined();
  });

  it.each([
    [400, /not right/],
    [404, /expired/],
  ])("names a %i outcome", async (status, message) => {
    stubFetch(jsonResponse(status, { error: "whatever" }));

    await expect(
      verifyRegistrationViaGateway(pending(2), "123456"),
    ).rejects.toThrow(message);
  });

  it("reports a rate limit with when the block ends", async () => {
    vi.useFakeTimers({ now: 1_000_000 });
    stubFetch(jsonResponse(429, { error: "OTP request limit exceeded" }));

    const err = await verifyRegistrationViaGateway(pending(0), "123456").catch(
      (e: unknown) => e,
    );

    expect(err).toBeInstanceOf(OtpBlockedError);
    expect((err as OtpBlockedError).blockedUntil).toBe(1_000_000 + TEN_MINUTES);
  });

  it("sends nothing for a malformed code", async () => {
    const fetchMock = stubFetch();

    await expect(
      verifyRegistrationViaGateway(pending(2), "12ab"),
    ).rejects.toBeInstanceOf(AuthError);
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

describe("resendOtpViaGateway", () => {
  it("posts the email to /auth/resend-otp and counts the replacement", async () => {
    vi.useFakeTimers({ now: 2_000_000 });
    const fetchMock = stubFetch(jsonResponse(200, {}));

    const next = await resendOtpViaGateway(pending(2));

    const { url, body } = request(fetchMock);
    expect(url).toMatch(/\/auth\/resend-otp$/);
    expect(body).toEqual({ email: EMAIL });
    expect(next.resendsRemaining).toBe(1);
    expect(next.expiresAt).toBe(2_000_000 + FIVE_MINUTES);
  });

  it("leaves an unknown count unknown", async () => {
    stubFetch(jsonResponse(200, {}));

    expect((await resendOtpViaGateway(pending(null))).resendsRemaining).toBeNull();
  });

  it.each([
    [404, /no account/i],
    [409, /already verified/i],
  ])("names a %i outcome", async (status, message) => {
    stubFetch(jsonResponse(status, { error: "whatever" }));

    await expect(resendOtpViaGateway(pending(2))).rejects.toThrow(message);
  });

  it("reports a rate limit as OtpBlockedError", async () => {
    stubFetch(jsonResponse(429, { error: "OTP request limit exceeded" }));

    await expect(resendOtpViaGateway(pending(0))).rejects.toBeInstanceOf(
      OtpBlockedError,
    );
  });
});

describe("resumeVerificationViaGateway", () => {
  it("requests a fresh code and does not guess the replacement count", async () => {
    const fetchMock = stubFetch(jsonResponse(200, {}));

    const opened = await resumeVerificationViaGateway(` ${EMAIL} `);

    expect(request(fetchMock).body).toEqual({ email: EMAIL });
    expect(opened.email).toBe(EMAIL);
    expect(opened.resendsRemaining).toBeNull();
  });
});

describe("logInViaGateway on an unverified account", () => {
  it("rejects with UnverifiedAccountError", async () => {
    stubFetch(jsonResponse(403, { error: "ACCOUNT_UNVERIFIED" }));

    await expect(logInViaGateway(EMAIL, "Passw0rd")).rejects.toBeInstanceOf(
      UnverifiedAccountError,
    );
  });
});
