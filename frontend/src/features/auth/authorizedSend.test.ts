// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-22
// Scope: Tests for the D-033 refresh path — on demand rather than on page
//   load, and serialised so two callers cannot spend the cookie twice.
// Author review: PENDING — <reviewer to complete>

import { beforeEach, describe, expect, it, vi } from "vitest";
import { createAuthorizedSend } from "./session";
import { createTokenStore } from "../../lib/tokens";

/** A fetch that records calls and answers with whatever is queued. */
function stubFetch(statuses: number[]) {
  const calls: { authorization: string | null }[] = [];
  let i = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init?: RequestInit) => {
      const headers = new Headers(init?.headers);
      calls.push({ authorization: headers.get("Authorization") });
      const status = statuses[Math.min(i++, statuses.length - 1)];
      return new Response(JSON.stringify({ ok: status < 400 }), {
        status,
        headers: { "Content-Type": "application/json" },
      });
    }),
  );
  return calls;
}

describe("createAuthorizedSend (D-033)", () => {
  beforeEach(() => {
    vi.unstubAllGlobals();
  });

  it("refreshes on demand when no access token is in memory", async () => {
    // Exactly the state after a page reload: the cookie is live, the closure
    // is empty. The session must come back without the user logging in again.
    const tokens = createTokenStore();
    const refresh = vi.fn(async () => ({ accessToken: "fresh" }));
    const calls = stubFetch([200]);

    const authorizedSend = createAuthorizedSend({
      tokens,
      refresh,
      onSessionExpired: () => {},
    });
    const res = await authorizedSend({ baseUrl: "", path: "/api/suppliers" });

    expect(refresh).toHaveBeenCalledTimes(1);
    expect(refresh).toHaveBeenCalledWith();
    expect(res.status).toBe(200);
    expect(calls[0].authorization).toBe("Bearer fresh");
  });

  it("does not refresh when a token is already in memory", async () => {
    // "On demand, not on every page load" — a live token means no round trip.
    const tokens = createTokenStore();
    tokens.setPair({ accessToken: "live" });
    const refresh = vi.fn(async () => ({ accessToken: "fresh" }));
    const calls = stubFetch([200]);

    const authorizedSend = createAuthorizedSend({
      tokens,
      refresh,
      onSessionExpired: () => {},
    });
    await authorizedSend({ baseUrl: "", path: "/api/suppliers" });

    expect(refresh).not.toHaveBeenCalled();
    expect(calls[0].authorization).toBe("Bearer live");
  });

  it("spends the cookie once when several requests start together", async () => {
    // user-service rotates the refresh token and treats a replay as a stolen
    // session, revoking every session for that user. Three parallel requests
    // must not become three exchanges.
    //
    // NOTE: this covers the in-page in-flight promise, not the Web Lock. The
    // CROSS-TAB guarantee cannot be exercised here — vitest runs one context
    // and `navigator.locks` is absent in it, so underRefreshLock falls back to
    // calling through. Two real tabs are still untested.
    const tokens = createTokenStore();
    let issued = 0;
    const refresh = vi.fn(async () => {
      issued += 1;
      await new Promise((r) => setTimeout(r, 5));
      return { accessToken: `fresh-${issued}` };
    });
    stubFetch([200]);

    const authorizedSend = createAuthorizedSend({
      tokens,
      refresh,
      onSessionExpired: () => {},
    });
    await Promise.all([
      authorizedSend({ baseUrl: "", path: "/api/suppliers" }),
      authorizedSend({ baseUrl: "", path: "/api/orders" }),
      authorizedSend({ baseUrl: "", path: "/api/credits" }),
    ]);

    expect(refresh).toHaveBeenCalledTimes(1);
  });

  it("retries once on a 401 and gives up on a second", async () => {
    const tokens = createTokenStore();
    tokens.setPair({ accessToken: "stale" });
    const refresh = vi.fn(async () => ({ accessToken: "fresh" }));
    const calls = stubFetch([401, 401]);

    const authorizedSend = createAuthorizedSend({
      tokens,
      refresh,
      onSessionExpired: () => {},
    });
    const res = await authorizedSend({ baseUrl: "", path: "/api/suppliers" });

    expect(refresh).toHaveBeenCalledTimes(1);
    expect(calls).toHaveLength(2);
    expect(calls[1].authorization).toBe("Bearer fresh");
    // A second 401 is a real authorization failure, not an expiry. Returning
    // it rather than looping is the point.
    expect(res.status).toBe(401);
  });

  it("ends the session when the cookie is gone", async () => {
    // The gateway rejects the exchange: cookie expired, cleared by logout, or
    // already spent. There is nothing to retry.
    const tokens = createTokenStore();
    const onSessionExpired = vi.fn();
    const refresh = vi.fn(async () => {
      throw new Error("Your session has expired.");
    });
    stubFetch([200]);

    const authorizedSend = createAuthorizedSend({
      tokens,
      refresh,
      onSessionExpired,
    });
    const res = await authorizedSend({ baseUrl: "", path: "/api/suppliers" });

    expect(onSessionExpired).toHaveBeenCalledTimes(1);
    expect(res.status).toBe(401);
    expect(tokens.getAccessToken()).toBeNull();
  });
});
