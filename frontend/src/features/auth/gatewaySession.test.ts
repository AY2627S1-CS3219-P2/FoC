// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-21
// Scope: Tests for the session the gateway client builds out of a token pair
//   plus the profile call — specifically the email/phone_num mapping added
//   when user-service's UserResponse gained both fields (72fe5a5).
// Author review: PENDING — <reviewer to complete>

import { afterEach, describe, expect, it, vi } from "vitest";

import { logInViaGateway } from "./authApi";

/**
 * These tests stub `fetch` rather than run against a gateway. What they cover
 * is this folder's own mapping — which profile field lands on which Session
 * field, and what happens when the profile call cannot answer. Whether
 * user-service really returns `phone_num` is its own service's test to write;
 * here it is transcribed from its committed `api/openapi.yaml`.
 */

/** An unsigned token carrying just the two claims the UI reads (lib/jwt.ts). */
function fakeAccessToken(claims: Record<string, unknown>): string {
  const encode = (value: unknown) =>
    btoa(JSON.stringify(value)).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
  return `${encode({ alg: "RS256", typ: "JWT" })}.${encode(claims)}.not-a-signature`;
}

const UID = "3f2b1c4d-0000-4000-8000-000000000001";

function jsonResponse(status: number, body: unknown) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: () => Promise.resolve(body),
  } as unknown as Response;
}

/**
 * Answers the login POST with a token pair, then the profile GET with
 * whatever `profile` is — or a failure status when it is null.
 */
function stubGateway(profile: Record<string, unknown> | null, profileStatus = 200) {
  const tokens = {
    accessToken: fakeAccessToken({ sub: UID, role: "STUDENT" }),
    refreshToken: "refresh-token",
  };
  const fetchMock = vi
    .fn()
    .mockResolvedValueOnce(jsonResponse(200, tokens))
    .mockResolvedValueOnce(
      profile === null
        ? jsonResponse(profileStatus, { error: "profile unavailable" })
        : jsonResponse(200, profile),
    );
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("logInViaGateway", () => {
  it("fills email and contact from the profile response", async () => {
    stubGateway({
      uid: UID,
      username: "nigeltzy",
      email: "nigeltzy@u.nus.edu",
      phone_num: "+6591234567",
    });

    const { session } = await logInViaGateway("nigeltzy", "Password1");

    expect(session.userId).toBe(UID);
    expect(session.username).toBe("nigeltzy");
    // F1.4.1 — `phone_num` is what the backlog calls contact information.
    expect(session.email).toBe("nigeltzy@u.nus.edu");
    expect(session.contact).toBe("+6591234567");
  });

  it("takes the role from the token, not the profile", async () => {
    // RestrictedUserResponse omits account_role entirely, which is what an
    // ordinary STUDENT receives. The claim has to carry it.
    stubGateway({
      uid: UID,
      username: "nigeltzy",
      email: "nigeltzy@u.nus.edu",
      phone_num: "+6591234567",
    });

    const { session } = await logInViaGateway("nigeltzy", "Password1");

    expect(session.role).toBe("STUDENT");
  });

  it("treats a blank phone_num as absent rather than empty", async () => {
    stubGateway({
      uid: UID,
      username: "nigeltzy",
      email: "nigeltzy@u.nus.edu",
      phone_num: "   ",
    });

    const { session } = await logInViaGateway("nigeltzy", "Password1");

    expect(session.contact).toBeUndefined();
    expect(session.email).toBe("nigeltzy@u.nus.edu");
  });

  it("still logs in when the profile call fails", async () => {
    // The tokens are already valid at this point; a profile we could not read
    // is a degraded display, not a failed login.
    stubGateway(null, 500);

    const { session, tokens } = await logInViaGateway("nigeltzy", "Password1");

    // Only the access token reaches this code: the gateway lifts the refresh
    // token into its HttpOnly cookie and strips it from the body (D-033).
    expect(typeof tokens.accessToken).toBe("string");
    expect(session.userId).toBe(UID);
    expect(session.username).toBe("nigeltzy");
    expect(session.email).toBeUndefined();
    expect(session.contact).toBeUndefined();
  });

  it("falls back to the typed identifier's local part for the username", async () => {
    stubGateway(null, 404);

    const { session } = await logInViaGateway("nigeltzy@u.nus.edu", "Password1");

    expect(session.username).toBe("nigeltzy");
  });
});
