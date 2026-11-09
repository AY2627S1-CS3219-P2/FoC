// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-09
// Scope: Tests for the credit-service admin client (D-150 to D-153) — paths,
//   methods and query, the no-wallet 404 against other failures, the response
//   checks, and the wording of what a Re-verify did — against a stubbed
//   authorized transport.
// Author review: PENDING — <reviewer to complete>

import { describe, expect, it, vi } from "vitest";
import type { AuthorizedSend } from "../auth/session";
import { NetworkError, type HttpResponse } from "../../lib/http";
import { CreditAccountsApiError } from "./creditAccountsApi";
import { createCreditAdminApi } from "./creditAdminApi";
import {
  amountsText,
  parseAdminWallet,
  parseFlaggedPage,
  parseReverifyResult,
  reasonLabel,
  reverifyMessage,
  type AdminWallet,
} from "./creditAdminTypes";

const USER_ID = "3f2b8c1e-1d2a-4b5c-8d9e-0a1b2c3d4e5f";

const FLAGGED = {
  userId: USER_ID,
  reasonCode: "AVAILABLE_PROJECTION_MISMATCH",
  detail: "ledger available sum differs from account projection",
  expected: 100,
  observed: 105,
  flaggedAt: "2026-11-09T08:00:00Z",
};

const WALLET: AdminWallet = {
  userId: USER_ID,
  availableCredits: 105,
  reservedCredits: 0,
  integrityStatus: "FLAGGED",
  flaggedAt: "2026-11-09T08:00:00Z",
  reasonCode: "AVAILABLE_PROJECTION_MISMATCH",
  detail: null,
  expected: 100,
  observed: 105,
  createdAt: "2026-11-01T08:00:00Z",
  updatedAt: "2026-11-09T08:00:00Z",
};

/** A transport that records each request and answers with `response`. */
function stubSend(response: HttpResponse | Error) {
  const send = vi.fn<AuthorizedSend>(async () => {
    if (response instanceof Error) throw response;
    return response;
  });
  return { send, api: createCreditAdminApi(send) };
}

describe("creditAdminApi requests", () => {
  it("lists the flagged accounts with GET and the paging query", async () => {
    const page = { items: [FLAGGED], total: 1, limit: 50, offset: 0 };
    const { send, api } = stubSend({ ok: true, status: 200, body: page });
    await expect(api.listFlaggedAccounts(50, 0)).resolves.toEqual(page);
    expect(send.mock.calls[0]![0]).toMatchObject({ path: "/api/credits/admin/flagged-accounts?limit=50", method: "GET" });

    await api.listFlaggedAccounts(50, 100);
    expect(send.mock.calls[1]![0].path).toBe("/api/credits/admin/flagged-accounts?limit=50&offset=100");
  });

  it("reads one wallet and encodes the id into the path", async () => {
    const { send, api } = stubSend({ ok: true, status: 200, body: WALLET });
    await expect(api.getWallet(USER_ID)).resolves.toEqual(WALLET);
    expect(send.mock.calls[0]![0].path).toBe(`/api/credits/admin/users/${USER_ID}/account`);
    await api.getWallet("a/b");
    expect(send.mock.calls[1]![0].path).toBe("/api/credits/admin/users/a%2Fb/account");
  });

  it("answers null for a user with no wallet, but throws for any other 404", async () => {
    const none = stubSend({ ok: false, status: 404, body: { code: "WALLET_NOT_READY", message: "the user has no credit account" } });
    await expect(none.api.getWallet(USER_ID)).resolves.toBeNull();

    const missing = stubSend({ ok: false, status: 404, body: null });
    await expect(missing.api.getWallet(USER_ID)).rejects.toBeInstanceOf(CreditAccountsApiError);
  });

  it("re-verifies with POST and no body", async () => {
    const { send, api } = stubSend({ ok: true, status: 200, body: { outcome: "CLEARED" } });
    await expect(api.reverify(USER_ID)).resolves.toEqual({ outcome: "CLEARED" });
    const options = send.mock.calls[0]![0];
    expect(options).toMatchObject({ path: `/api/credits/admin/users/${USER_ID}/reconcile`, method: "POST" });
    expect(options.body).toBeUndefined();
  });

  it("reads the credit audit log as camelCase rows from the credits source", async () => {
    const row = {
      id: "b1",
      occurredAt: "2026-11-09T09:00:00Z",
      actorId: "admin-1",
      actorRole: "ADMIN",
      action: "credit.reverify",
      targetType: "account",
      targetId: USER_ID,
      outcome: "succeeded",
      refusalReason: "",
      reason: "",
      beforeState: { integrity_status: "FLAGGED" },
      afterState: { integrity_status: "CONSISTENT" },
      correlationId: "req-1",
    };
    const { send, api } = stubSend({ ok: true, status: 200, body: { items: [row], total: 1, limit: 50, offset: 0 } });
    const page = await api.listAuditLog({ targetId: USER_ID, limit: 10 });
    expect(send.mock.calls[0]![0].path).toBe(`/api/credits/admin/audit-log?target_id=${USER_ID}&limit=10`);
    expect(page.items[0]).toMatchObject({ source: "credits", action: "credit.reverify", targetId: USER_ID });
  });
});

describe("creditAdminApi failures", () => {
  it("keeps credit-service's message and status, including the 403 for a non-admin", async () => {
    const { api } = stubSend({ ok: false, status: 403, body: { code: "FORBIDDEN", message: "admin access required" } });
    await expect(api.listFlaggedAccounts(50, 0)).rejects.toMatchObject({
      name: "CreditAccountsApiError",
      message: "admin access required",
      status: 403,
    });
  });

  it("reads the gateway's own {error} shape too", async () => {
    const { api } = stubSend({ ok: false, status: 404, body: { error: "no such route" } });
    await expect(api.reverify(USER_ID)).rejects.toMatchObject({ message: "no such route", status: 404 });
  });

  it("turns a request that never arrived into status 0", async () => {
    const { api } = stubSend(new NetworkError(""));
    const err = await api.reverify(USER_ID).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(CreditAccountsApiError);
    expect((err as CreditAccountsApiError).status).toBe(0);
  });

  it.each([
    ["a flagged page", (api: ReturnType<typeof createCreditAdminApi>) => api.listFlaggedAccounts(50, 0)],
    ["a wallet", (api: ReturnType<typeof createCreditAdminApi>) => api.getWallet(USER_ID)],
    ["a re-verify answer", (api: ReturnType<typeof createCreditAdminApi>) => api.reverify(USER_ID)],
    ["an audit page", (api: ReturnType<typeof createCreditAdminApi>) => api.listAuditLog()],
  ])("refuses a 200 that is not %s", async (_name, call) => {
    const { api } = stubSend({ ok: true, status: 200, body: { nope: true } });
    await expect(call(api)).rejects.toMatchObject({ status: 0 });
  });
});

describe("response checks", () => {
  it("accepts absent or null optional fields and refuses wrong types", () => {
    expect(parseFlaggedPage({ items: [{ userId: "u", reasonCode: "X", flaggedAt: "t" }], total: 1, limit: 50, offset: 0 })).not.toBeNull();
    expect(parseFlaggedPage({ items: [{ ...FLAGGED, observed: "105" }], total: 1, limit: 50, offset: 0 })).toBeNull();
    expect(parseFlaggedPage({ items: [], total: "0", limit: 50, offset: 0 })).toBeNull();
    expect(parseAdminWallet({ ...WALLET, integrityStatus: "BROKEN" })).toBeNull();
    expect(parseAdminWallet({ ...WALLET, flaggedAt: null, reasonCode: null, integrityStatus: "CONSISTENT" })).not.toBeNull();
    expect(parseReverifyResult({ outcome: "MAYBE" })).toBeNull();
    expect(parseReverifyResult({ outcome: "STILL_FLAGGED", reasonCode: "X", expected: 1, observed: 2 })).not.toBeNull();
  });
});

describe("wording", () => {
  it("explains known reason codes and passes unknown ones through", () => {
    expect(reasonLabel("OUT_OF_BOUNDS_BALANCE")).toBe("A balance is outside the allowed range");
    expect(reasonLabel("SOMETHING_NEW")).toBe("SOMETHING_NEW");
    expect(reasonLabel(null)).toBe("Unknown reason");
  });

  it("words the two amounts a finding compares", () => {
    expect(amountsText(100, 105)).toBe("Expected 100, observed 105");
    expect(amountsText(undefined, 2_000_000_000)).toBe("Observed 2000000000");
    expect(amountsText(null, null)).toBeNull();
    expect(amountsText(10.5, 10.25)).toBe("Expected 10.5, observed 10.25");
  });

  it("says what each Re-verify outcome means, and that clearing does not replay a blocked settlement", () => {
    const cleared = reverifyMessage({ outcome: "CLEARED" }, "alice");
    expect(cleared.kind).toBe("success");
    expect(cleared.text).toContain("no longer flagged");
    expect(cleared.text).toContain("not replayed");

    const still = reverifyMessage(
      { outcome: "STILL_FLAGGED", reasonCode: "AVAILABLE_PROJECTION_MISMATCH", expected: 100, observed: 105 },
      "alice",
    );
    expect(still.kind).toBe("error");
    expect(still.text).toContain("still flagged");
    expect(still.text).toContain("expected 100, observed 105");

    expect(reverifyMessage({ outcome: "NOT_FLAGGED" }, "alice")).toMatchObject({ kind: "info" });
    expect(reverifyMessage({ outcome: "STALE" }, "alice").text).toContain("Try again");
  });
});
