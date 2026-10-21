// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-18
// Scope: Tests for the credit-service account client — paths and the paging
//   query, the "no account yet" 404 against a 404 from a route that is not
//   there, other failures, and the response checks — against a stubbed
//   authorized transport. Moved to cursor paging and {code, message} errors
//   on 2026-10-21.
// Author review: James Teo — reviewed and verified.

import { describe, expect, it, vi } from "vitest";
import type { AuthorizedSend } from "../auth/session";
import { NetworkError, type HttpResponse } from "../../lib/http";
import { createCreditAccountsApi, CreditAccountsApiError } from "./creditAccountsApi";
import type { AccountResponse, TransactionListResponse } from "./creditTypes";

const USER_ID = "3f2b8c1e-1d2a-4b5c-8d9e-0a1b2c3d4e5f";

const ACCOUNT: AccountResponse = {
  userId: USER_ID,
  creditStatus: "READY",
  availableCredits: 90,
  reservedCredits: 10,
  updatedAt: "2026-10-18T08:30:00Z",
};

const PAGE: TransactionListResponse = {
  items: [
    {
      operationId: "a1b2c3d4-0000-4000-8000-000000000002",
      errandId: "6b1f0c8e-2a3d-4e5f-9a0b-1c2d3e4f5a6b",
      reason: "ERRAND_RESERVATION",
      availableDelta: -10,
      reservedDelta: 10,
      occurredAt: null,
      createdAt: "2026-10-18T08:30:00Z",
    },
    {
      operationId: "a1b2c3d4-0000-4000-8000-000000000001",
      errandId: null,
      reason: "REGISTRATION_ALLOCATION",
      availableDelta: 100,
      reservedDelta: 0,
      occurredAt: "2026-10-18T08:00:00Z",
      createdAt: "2026-10-18T08:00:00Z",
    },
  ],
  nextCursor: null,
};

/** credit-service's answer for a user with no account: the spec's ErrorResponse. */
const NO_ACCOUNT: HttpResponse = {
  ok: false,
  status: 404,
  body: { code: "WALLET_NOT_READY", message: "wallet is not ready" },
};

/** A 404 with no JSON body, as a service that does not serve the route yet sends. */
const ROUTE_MISSING: HttpResponse = { ok: false, status: 404, body: null };

/** A transport that records each request and answers with `response`. */
function stubSend(response: HttpResponse | Error) {
  const send = vi.fn<AuthorizedSend>(async () => {
    if (response instanceof Error) throw response;
    return response;
  });
  return { send, api: createCreditAccountsApi(send) };
}

describe("creditAccountsApi requests", () => {
  it("reads the account with GET /api/credits/accounts/me", async () => {
    const { send, api } = stubSend({ ok: true, status: 200, body: ACCOUNT });
    await expect(api.getAccount()).resolves.toEqual(ACCOUNT);
    expect(send).toHaveBeenCalledTimes(1);
    const options = send.mock.calls[0]![0];
    expect(options).toMatchObject({ baseUrl: "", path: "/api/credits/accounts/me" });
    expect(options.method ?? "GET").toBe("GET");
    expect(options.body).toBeUndefined();
  });

  it.each([
    [20, undefined, "/api/credits/accounts/me/transactions?limit=20"],
    [5, "eyJjIjoxfQ", "/api/credits/accounts/me/transactions?limit=5&cursor=eyJjIjoxfQ"],
  ])("pages the transactions with limit %i and cursor %s", async (limit, cursor, path) => {
    const { send, api } = stubSend({ ok: true, status: 200, body: PAGE });
    await expect(api.listTransactions(limit, cursor)).resolves.toEqual(PAGE);
    const options = send.mock.calls[0]![0];
    expect(options.path).toBe(path);
    expect(options.method ?? "GET").toBe("GET");
  });
});

describe("creditAccountsApi and a user with no account", () => {
  it("answers null for the account", async () => {
    const { api } = stubSend(NO_ACCOUNT);
    await expect(api.getAccount()).resolves.toBeNull();
  });

  it("answers an empty last page for the transactions", async () => {
    const { api } = stubSend(NO_ACCOUNT);
    await expect(api.listTransactions(20)).resolves.toEqual({ items: [], nextCursor: null });
  });

  it("treats a 404 with another error code as unavailable", async () => {
    const { api } = stubSend({ ok: false, status: 404, body: { code: "RESERVATION_NOT_FOUND", message: "x" } });
    await expect(api.getAccount()).rejects.toMatchObject({ status: 404 });
  });

  it.each([
    ["the account", (api: ReturnType<typeof createCreditAccountsApi>) => api.getAccount()],
    ["the transactions", (api: ReturnType<typeof createCreditAccountsApi>) => api.listTransactions(20)],
  ])("treats a 404 without the spec's body as unavailable for %s", async (_name, call) => {
    const { api } = stubSend(ROUTE_MISSING);
    await expect(call(api)).rejects.toMatchObject({
      name: "CreditAccountsApiError",
      status: 404,
      message: "Request failed (404)",
    });
  });
});

describe("creditAccountsApi failures", () => {
  it("keeps the gateway's message and status", async () => {
    const { api } = stubSend({ ok: false, status: 502, body: { error: "upstream service unavailable" } });
    await expect(api.getAccount()).rejects.toMatchObject({
      message: "upstream service unavailable",
      status: 502,
    });
  });

  it("keeps credit-service's message and status", async () => {
    const { api } = stubSend({
      ok: false,
      status: 503,
      body: { code: "SERVICE_UNAVAILABLE", message: "credit service unavailable" },
    });
    await expect(api.listTransactions(20)).rejects.toMatchObject({
      message: "credit service unavailable",
      status: 503,
    });
  });

  it("keeps a 401's status, which pauses a poller", async () => {
    const { api } = stubSend({ ok: false, status: 401, body: { error: "invalid token" } });
    await expect(api.listTransactions(20)).rejects.toMatchObject({ status: 401 });
  });

  it("falls back to the status when the body has no error field", async () => {
    const { api } = stubSend({ ok: false, status: 500, body: null });
    await expect(api.getAccount()).rejects.toThrow("Request failed (500)");
  });

  it("turns a request that never arrived into status 0", async () => {
    const { api } = stubSend(new NetworkError(""));
    const err = await api.getAccount().catch((e: unknown) => e);
    expect(err).toBeInstanceOf(CreditAccountsApiError);
    expect((err as CreditAccountsApiError).status).toBe(0);
  });

  const malformed: [string, (api: ReturnType<typeof createCreditAccountsApi>) => Promise<unknown>, unknown][] = [
    ["an account without its balance", (api) => api.getAccount(), { userId: USER_ID, creditStatus: "READY" }],
    [
      "an account credit-service reports as temporarily unavailable",
      (api) => api.getAccount(),
      { userId: USER_ID, creditStatus: "TEMPORARILY_UNAVAILABLE", updatedAt: ACCOUNT.updatedAt },
    ],
    [
      "a temporarily unavailable account that still sends figures",
      (api) => api.getAccount(),
      { ...ACCOUNT, creditStatus: "TEMPORARILY_UNAVAILABLE" },
    ],
    ["an empty account body", (api) => api.getAccount(), null],
    ["a page without items", (api) => api.listTransactions(20), { nextCursor: null }],
    ["a page whose cursor is not a string", (api) => api.listTransactions(20), { items: [], nextCursor: 5 }],
    [
      "a page whose delta is not a number",
      (api) => api.listTransactions(20),
      { ...PAGE, items: [{ ...PAGE.items[0], availableDelta: "-10" }] },
    ],
    [
      "a page with a row in the old shape",
      (api) => api.listTransactions(20),
      { items: [{ id: "x", amount: -10 }], nextCursor: null },
    ],
  ];

  it.each(malformed)("turns %s into an error with status 0", async (_name, call, body) => {
    const { api } = stubSend({ ok: true, status: 200, body });
    const err = await call(api).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(CreditAccountsApiError);
    expect((err as CreditAccountsApiError).status).toBe(0);
  });
});
