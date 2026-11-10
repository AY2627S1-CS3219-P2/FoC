// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-05
// Scope: Tests for the notification-service client against a stubbed authorized
//   transport: paths, the paging query, failures and the response checks.
// Author review: nigeltzy - Checked that the unit test implementation seems correct and as expected according to requirements.

import { describe, expect, it, vi } from "vitest";
import type { AuthorizedSend } from "../auth/session";
import { NetworkError, type HttpResponse } from "../../lib/http";
import { createNotificationsApi, NotificationsApiError } from "./notificationsApi";

const NOTIFICATION = {
  id: "6b1f0c8e-2a3d-4e5f-9a0b-1c2d3e4f5a6b",
  errandId: "3f2b8c1e-1d2a-4b5c-8d9e-0a1b2c3d4e5f",
  stateChange: "errand.accepted",
  message: "A courier accepted your errand.",
  read: false,
  createdAt: "2026-11-05T08:30:00Z",
};

function stub(response: Partial<HttpResponse>) {
  const send = vi.fn<AuthorizedSend>().mockResolvedValue({
    ok: true,
    status: 200,
    body: null,
    ...response,
  });
  return { send, api: createNotificationsApi(send) };
}

describe("list", () => {
  it("asks the gateway for one page", async () => {
    const page = { notifications: [NOTIFICATION], total: 1 };
    const { send, api } = stub({ body: page });

    await expect(api.list(50, 0)).resolves.toEqual(page);
    expect(send).toHaveBeenCalledWith(
      expect.objectContaining({ path: "/api/notifications?limit=50&offset=0", method: "GET" }),
    );
  });

  it("throws the service's message on a failure", async () => {
    const { api } = stub({ ok: false, status: 503, body: { error: "unavailable" } });
    await expect(api.list(50, 0)).rejects.toMatchObject({ message: "unavailable", status: 503 });
  });

  it("keeps the status when the body has no message", async () => {
    const { api } = stub({ ok: false, status: 502, body: null });
    await expect(api.list(50, 0)).rejects.toMatchObject({ status: 502 });
  });

  it("refuses a body that is not the contract's", async () => {
    const { api } = stub({ body: { notifications: [{ id: "x" }], total: 1 } });
    await expect(api.list(50, 0)).rejects.toBeInstanceOf(NotificationsApiError);
  });

  it("turns a request that never arrived into status 0", async () => {
    const send = vi.fn<AuthorizedSend>().mockRejectedValue(new NetworkError(""));
    await expect(createNotificationsApi(send).list(50, 0)).rejects.toMatchObject({ status: 0 });
  });
});

describe("markRead", () => {
  it("posts to the notification's read route", async () => {
    const { send, api } = stub({ status: 204 });

    await api.markRead("abc");
    expect(send).toHaveBeenCalledWith(
      expect.objectContaining({ path: "/api/notifications/abc/read", method: "POST" }),
    );
  });

  it("throws when the notification is not the user's", async () => {
    const { api } = stub({ ok: false, status: 404, body: { error: "no such notification" } });
    await expect(api.markRead("abc")).rejects.toMatchObject({ status: 404 });
  });
});
