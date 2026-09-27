// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-09-27
// Scope: Tests for the gateway client's refresh, page-load restore and logout
//   calls, against a stubbed fetch.
// Author review: nigeltzy

import { afterEach, describe, expect, it, vi } from "vitest";

import {
  AuthError,
  logOutViaGateway,
  refreshAccessTokenViaGateway,
  restoreSessionViaGateway,
} from "./authApi";

/**
 * These tests stub `fetch`, as gatewaySession.test.ts does. The page-load
 * restore's refresh lock is covered in restoreLock.test.ts.
 */

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

/** The URL and RequestInit of the fetch call at `index`. */
function request(fetchMock: ReturnType<typeof vi.fn>, index = 0) {
  const [url, init] = fetchMock.mock.calls[index] as [string, RequestInit];
  return { url, init, headers: new Headers(init.headers) };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("refreshAccessTokenViaGateway", () => {
  it("posts to /auth/refresh with no body and no bearer token", async () => {
    const fetchMock = stubFetch(jsonResponse(200, { accessToken: "at-2" }));

    const tokens = await refreshAccessTokenViaGateway();

    expect(tokens).toEqual({ accessToken: "at-2" });
    const { url, init, headers } = request(fetchMock);
    expect(url).toMatch(/\/auth\/refresh$/);
    expect(init.method).toBe("POST");
    // The refresh token is the HttpOnly cookie; the browser sends it, not us.
    expect(init.body).toBeUndefined();
    expect(headers.has("Authorization")).toBe(false);
  });

  it("rejects with AuthError when the gateway refuses the cookie", async () => {
    stubFetch(jsonResponse(401, { error: "session expired" }));

    await expect(refreshAccessTokenViaGateway()).rejects.toBeInstanceOf(AuthError);
  });

  it("rejects with AuthError when the answer carries no access token", async () => {
    stubFetch(jsonResponse(200, {}));

    await expect(refreshAccessTokenViaGateway()).rejects.toThrow(
      "The server did not return a new access token.",
    );
  });
});

describe("restoreSessionViaGateway", () => {
  it("returns null, without a profile call, when there is no live session", async () => {
    const fetchMock = stubFetch(jsonResponse(401, { error: "no session" }));

    await expect(restoreSessionViaGateway()).resolves.toBeNull();
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("returns null when the gateway cannot be reached", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new TypeError("Failed to fetch")));

    await expect(restoreSessionViaGateway()).resolves.toBeNull();
  });
});

describe("logOutViaGateway", () => {
  it("posts to /auth/logout with the access token and no body", async () => {
    const fetchMock = stubFetch(jsonResponse(204, null));

    await logOutViaGateway("at-1");

    const { url, init, headers } = request(fetchMock);
    expect(url).toMatch(/\/auth\/logout$/);
    expect(init.method).toBe("POST");
    // The gateway adds the refresh token from its cookie.
    expect(init.body).toBeUndefined();
    expect(headers.get("Authorization")).toBe("Bearer at-1");
  });

  it("resolves when the gateway answers with an error", async () => {
    stubFetch(jsonResponse(500, { error: "internal server error" }));

    await expect(logOutViaGateway("at-1")).resolves.toBeUndefined();
  });

  it("resolves when the gateway cannot be reached", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new TypeError("Failed to fetch")));

    await expect(logOutViaGateway("at-1")).resolves.toBeUndefined();
  });
});
