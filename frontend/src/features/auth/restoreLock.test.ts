// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-09-26
// Scope: Tests that the page-load session restore spends the refresh cookie
//   under the same cross-tab lock as createAuthorizedSend.
// Author review: nigeltzy — Checked test implementation seems valid and as expected.

import { afterEach, describe, expect, it, vi } from "vitest";
import { restoreSessionViaGateway } from "./authApi";

/**
 * The page-load restore spends the refresh cookie, so it needs the same
 * protection as createAuthorizedSend: user-service rotates the cookie on every
 * refresh, and a second request that presents the old one revokes every
 * session the user has.
 */

/** Must match REFRESH_LOCK in session.ts, or the two paths do not exclude each other. */
const REFRESH_LOCK = "foc.auth.refresh";
const UID = "3f2b1c4d-0000-4000-8000-000000000001";

/** An unsigned token carrying the `sub` claim sessionFromTokens reads. */
function fakeAccessToken(): string {
  const encode = (value: unknown) =>
    btoa(JSON.stringify(value)).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
  return `${encode({ alg: "RS256", typ: "JWT" })}.${encode({ sub: UID, role: "STUDENT" })}.sig`;
}

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

/**
 * A stand-in for navigator.locks that grants each name to one holder at a
 * time, in request order, like the real Web Locks API does across tabs.
 */
function fakeLocks() {
  const queues = new Map<string, Promise<unknown>>();
  const requested: string[] = [];
  let holders = 0;
  const request = vi.fn((name: string, ...rest: unknown[]) => {
    const callback = rest[rest.length - 1] as () => Promise<unknown>;
    requested.push(name);
    const previous = queues.get(name) ?? Promise.resolve();
    const run = previous.then(async () => {
      holders += 1;
      try {
        return await callback();
      } finally {
        holders -= 1;
      }
    });
    queues.set(name, run.catch(() => undefined));
    return run;
  });
  return { locks: { request }, requested, isHeld: () => holders > 0 };
}

/**
 * The gateway and user-service as the browser sees them: one cookie jar, a
 * refresh that rotates the cookie, and reuse detection that answers 401 and
 * revokes everything when a spent cookie comes back.
 */
function fakeGateway(isLockHeld: () => boolean) {
  let jar = "rt-0";
  let live: string | null = "rt-0";
  let issued = 0;
  const refreshes: { cookie: string; underLock: boolean; status: number }[] = [];

  const fetchMock = vi.fn(async (url: string) => {
    if (url.endsWith("/auth/refresh")) {
      const cookie = jar;
      const underLock = isLockHeld();
      await new Promise((resolve) => setTimeout(resolve, 5));
      if (cookie !== live) {
        live = null;
        refreshes.push({ cookie, underLock, status: 401 });
        return json(401, { error: "ErrSessionCompromised" });
      }
      issued += 1;
      live = jar = `rt-${issued}`;
      refreshes.push({ cookie, underLock, status: 200 });
      return json(200, { accessToken: fakeAccessToken() });
    }
    if (url.includes("/api/users/")) {
      return json(200, { uid: UID, username: "student1", email: "student1@u.nus.edu" });
    }
    return json(404, { error: "not found" });
  });
  return { fetchMock, refreshes };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("restoreSessionViaGateway", () => {
  it("spends the refresh cookie only while holding the refresh lock", async () => {
    const lock = fakeLocks();
    const gateway = fakeGateway(lock.isHeld);
    vi.stubGlobal("navigator", { locks: lock.locks });
    vi.stubGlobal("fetch", gateway.fetchMock);

    const restored = await restoreSessionViaGateway();

    expect(restored?.session.userId).toBe(UID);
    expect(lock.requested).toContain(REFRESH_LOCK);
    expect(gateway.refreshes).toHaveLength(1);
    expect(gateway.refreshes[0]!.underLock).toBe(true);
  });

  it("does not present the same cookie twice when two restores start together", async () => {
    // Two tabs opening at once, or StrictMode running the mount effect twice
    // under `npm run dev`.
    const lock = fakeLocks();
    const gateway = fakeGateway(lock.isHeld);
    vi.stubGlobal("navigator", { locks: lock.locks });
    vi.stubGlobal("fetch", gateway.fetchMock);

    const [first, second] = await Promise.all([
      restoreSessionViaGateway(),
      restoreSessionViaGateway(),
    ]);

    expect(gateway.refreshes.filter((r) => r.status === 401)).toEqual([]);
    expect(first?.session.userId).toBe(UID);
    expect(second?.session.userId).toBe(UID);
  });
});
