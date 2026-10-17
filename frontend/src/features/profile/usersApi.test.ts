// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-11
// Scope: Tests for the user-service profile client — the path, method and
//   body of each call, the UserResponse mapping, and how failures surface —
//   against a stubbed authorized transport.
// Author review: nigeltzy — Checked that the unit tests were generated and they
// seem valid and as expected.

import { describe, expect, it, vi } from "vitest";
import type { AuthorizedSend } from "../auth/session";
import { NetworkError, type HttpResponse } from "../../lib/http";
import { createUsersApi, toUserProfile, UsersApiError } from "./usersApi";

const UID = "3f2b8c1e-1d2a-4b5c-8d9e-0a1b2c3d4e5f";

/** The full UserResponse user-service sends to an ADMIN or after an update. */
const FULL = {
  uid: UID,
  username: "student2",
  email: "student2@u.nus.edu",
  phone_num: "91234567",
  account_role: "STUDENT",
  account_status: "ACTIVE",
  date_created: "2026-09-20T10:00:00Z",
};

/** A transport that records each request and answers with `response`. */
function stubSend(response: HttpResponse | Error) {
  const send = vi.fn<AuthorizedSend>(async () => {
    if (response instanceof Error) throw response;
    return response;
  });
  return { send, api: createUsersApi(send) };
}

describe("usersApi requests", () => {
  it("fetches a profile with GET /api/users/{uid}", async () => {
    const { send, api } = stubSend({ ok: true, status: 200, body: FULL });
    await api.getUser(UID);
    expect(send).toHaveBeenCalledWith(
      expect.objectContaining({ path: `/api/users/${UID}` }),
    );
    expect(send.mock.calls[0]![0].method ?? "GET").toBe("GET");
  });

  // Each update sends exactly one field, so a modal changes only its own.
  const updates: [string, (api: ReturnType<typeof createUsersApi>) => Promise<unknown>, object][] = [
    ["username", (api) => api.changeOwnUsername(UID, "  newname "), { username: "newname" }],
    ["contact", (api) => api.changeOwnContact(UID, " 98765432 "), { phone_num: "98765432" }],
    [
      "own password",
      (api) => api.changeOwnPassword(UID, "OldPass1", "NewPass1"),
      { currentPassword: "OldPass1", newPassword: "NewPass1" },
    ],
    ["admin reset", (api) => api.resetPassword(UID, "NewPass1"), { newPassword: "NewPass1" }],
  ];

  it.each(updates)("%s sends PUT with only its own field", async (_name, call, body) => {
    const { send, api } = stubSend({ ok: true, status: 200, body: FULL });
    await call(api);
    expect(send).toHaveBeenCalledTimes(1);
    expect(send.mock.calls[0]![0]).toMatchObject({
      path: `/api/users/${UID}`,
      method: "PUT",
    });
    expect(send.mock.calls[0]![0].body).toEqual(body);
  });

  it("encodes the account ID into the path", async () => {
    const { send, api } = stubSend({ ok: true, status: 200, body: FULL });
    await api.getUser("a/b");
    expect(send.mock.calls[0]![0].path).toBe("/api/users/a%2Fb");
  });
});

describe("toUserProfile", () => {
  it("maps a full UserResponse, renaming phone_num to contact", () => {
    expect(toUserProfile(FULL)).toEqual({
      uid: UID,
      username: "student2",
      email: "student2@u.nus.edu",
      contact: "91234567",
      role: "STUDENT",
      status: "ACTIVE",
      createdAt: "2026-09-20T10:00:00Z",
    });
  });

  it("leaves role, status and createdAt undefined for a restricted response", () => {
    const { uid, username, email, phone_num } = FULL;
    const profile = toUserProfile({ uid, username, email, phone_num });
    expect(profile.role).toBeUndefined();
    expect(profile.status).toBeUndefined();
    expect(profile.createdAt).toBeUndefined();
  });

  it("treats a blank contact and an unknown enum value as absent", () => {
    const profile = toUserProfile({ ...FULL, phone_num: "  ", account_role: "OWNER" });
    expect(profile.contact).toBeUndefined();
    expect(profile.role).toBeUndefined();
  });
});

describe("usersApi failures", () => {
  it("keeps user-service's error message and status", async () => {
    const { api } = stubSend({ ok: false, status: 409, body: { error: "username already taken" } });
    await expect(api.changeOwnUsername(UID, "taken")).rejects.toMatchObject({
      name: "UsersApiError",
      message: "username already taken",
      status: 409,
    });
  });

  it("falls back to the status when the body has no error field", async () => {
    const { api } = stubSend({ ok: false, status: 500, body: null });
    await expect(api.getUser(UID)).rejects.toThrow("Request failed (500)");
  });

  it("reports a 401 on a password change as a wrong current password", async () => {
    const { api } = stubSend({ ok: false, status: 401, body: { error: "invalid current password" } });
    await expect(api.changeOwnPassword(UID, "wrong", "NewPass1")).rejects.toThrow(
      "Your current password is incorrect.",
    );
  });

  it("does not call a 401 on any other route a password problem", async () => {
    const { api } = stubSend({ ok: false, status: 401, body: { error: "authentication required" } });
    await expect(api.getUser(UID)).rejects.toThrow("authentication required");
  });

  it("turns a request that never arrived into status 0", async () => {
    const { api } = stubSend(new NetworkError(""));
    const err = await api.getUser(UID).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(UsersApiError);
    expect((err as UsersApiError).status).toBe(0);
  });
});
