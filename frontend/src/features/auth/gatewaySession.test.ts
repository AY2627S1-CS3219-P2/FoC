// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-21
// Scope: Tests for the session the gateway client builds out of a token pair
//   plus the profile call — specifically the email/phone_num mapping added
//   when user-service's UserResponse gained both fields (72fe5a5).
// Author review: Nigeltzy - AI was used to create the implementation for the unit tests for the session and gateway client. Checked and is valid and works as intended.

import { afterEach, describe, expect, it, vi } from "vitest";

import { AuthError, logInViaGateway, SuspendedAccountError } from "./authApi";

/**
 * These tests stub `fetch`. They cover how the gateway client maps a profile
 * response onto Session, and what happens when the profile call fails.
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
 * whatever `profile` is — or a failure status when it is null. `tokenRole`
 * is the role claim in the access token.
 */
// AI-generated (edited by nigeltzy).
function stubGateway(
  profile: Record<string, unknown> | null,
  profileStatus = 200,
  tokenRole = "STUDENT",
) {
  const tokens = {
    accessToken: fakeAccessToken({ sub: UID, role: tokenRole }),
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

  // AI-generated (edited by nigeltzy).
  it("takes the role from the token, not the profile", async () => {
    // The two disagree, and neither is the STUDENT default, so the test fails
    // if the profile wins or if neither is read.
    stubGateway(
      {
        uid: UID,
        username: "nigeltzy",
        email: "nigeltzy@u.nus.edu",
        phone_num: "+6591234567",
        account_role: "STUDENT",
      },
      200,
      "ADMIN",
    );

    const { session } = await logInViaGateway("nigeltzy", "Password1");

    expect(session.role).toBe("ADMIN");
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

    // The stubbed body still carries a refreshToken; only the access token is
    // kept.
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

// AI-generated (edited by nigeltzy).
describe("logInViaGateway with a refused login", () => {
  it("names a suspended account from user-service's 403", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValueOnce(jsonResponse(403, { error: "account suspended" })),
    );

    const error = await logInViaGateway("nigeltzy", "Password1").catch((err: unknown) => err);

    expect(error).toBeInstanceOf(SuspendedAccountError);
    // The UI's own sentence, not the server's raw text.
    expect((error as Error).message).toBe("This account is suspended. Contact an administrator.");
  });

  it("does not treat any other 403 as a suspension", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValueOnce(jsonResponse(403, { error: "forbidden" })),
    );

    const error = await logInViaGateway("nigeltzy", "Password1").catch((err: unknown) => err);

    expect(error).toBeInstanceOf(AuthError);
    expect(error).not.toBeInstanceOf(SuspendedAccountError);
  });
});
