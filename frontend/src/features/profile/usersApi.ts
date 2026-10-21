// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-11
// Scope: Client for user-service's profile routes through the gateway —
//   fetching a profile by ID and the PUT /api/v1/users/{uid} updates, one
//   function per field. Request and response shapes are transcribed from
//   user-service/api/openapi.yaml on main (UpdateProfileRequest,
//   UserResponse, RestrictedUserResponse).
// Author review: nigeltzy — Checked that the API was generated according to
// the format and requirements that were prefviously discussed and set.

import type { AuthorizedSend } from "../auth/session";
import { config } from "../../lib/config";
// AI-generated (edited by James Teo): HttpResponse, for request() below.
import { NetworkError, type HttpResponse } from "../../lib/http";
import type { AccountRole } from "../auth/types";

/**
 * The only module that calls user-service's `/api/users/{uid}` routes. Login,
 * registration and refresh stay in auth/authApi.ts. Every call goes to the
 * gateway, which forwards it to `/api/v1/users/{uid}` with the bearer token
 * still attached (D-030), because user-service authenticates these routes
 * itself.
 */

/** The gateway's public prefix for user-service's user routes. */
const PREFIX = "/api/users";

/**
 * A user profile as user-service returns it, with `phone_num` renamed to the
 * backlog's `contact` (F1.4.1), the same rename authApi's session makes.
 *
 * `role`, `status` and `createdAt` are on UserResponse only. user-service
 * sends that to ADMIN callers and as the answer to an update; a non-admin
 * lookup gets RestrictedUserResponse, so those three are undefined.
 */
export interface UserProfile {
  uid: string;
  username: string;
  email?: string;
  contact?: string;
  role?: AccountRole;
  status?: AccountStatus;
  /** ISO 8601, as user-service sends `date_created`. */
  createdAt?: string;
}

/** user-service's `account_status` enum. */
export type AccountStatus = "ACTIVE" | "SUSPENDED" | "UNVERIFIED";

/**
 * An error from a user-service profile call. user-service answers
 * `{"error": "..."}` with a non-2xx status. `status` is 0 when no response
 * arrived.
 */
export class UsersApiError extends Error {
  readonly status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = "UsersApiError";
    this.status = status;
  }
}

/** The surface a generated OpenAPI client would expose for these routes. */
export interface UsersApi {
  /** Any signed-in caller may fetch any profile; user-service picks the fields by role. */
  getUser(uid: string): Promise<UserProfile>;
  changeOwnUsername(uid: string, username: string): Promise<UserProfile>;
  changeOwnContact(uid: string, contact: string): Promise<UserProfile>;
  /**
   * user-service checks `currentPassword` and, on success, revokes every
   * session the account has, the caller's own included.
   */
  changeOwnPassword(
    uid: string,
    currentPassword: string,
    newPassword: string,
  ): Promise<UserProfile>;
  /**
   * ADMIN only, and only for another account: user-service needs no current
   * password then. It revokes every session of the target account.
   */
  resetPassword(uid: string, newPassword: string): Promise<UserProfile>;
  // AI-generated (edited by James Teo): suspend and reactivate, user-service's
  // PATCH /api/v1/users/{uid}/status with an UpdateStatusRequest. Two named
  // functions rather than one taking the status, so no caller passes a flag.
  /**
   * ADMIN only. Sets the account SUSPENDED: user-service revokes every
   * session it has and refuses its sign-in until it is reactivated. A 409
   * means it is the last active admin. Resolves with nothing, because the
   * 200 has an empty body; fetch the profile again to see the new status.
   */
  suspendUser(uid: string): Promise<void>;
  /** ADMIN only. Sets the account ACTIVE again, so its password signs in. */
  reactivateUser(uid: string): Promise<void>;
}

/**
 * Builds the client over an authorized transport. App.tsx creates it once per
 * token store, alongside the suppliers client.
 *
 * Each update sends only the field it changes: user-service leaves an empty
 * or absent field unchanged, so one modal changes one thing.
 */
export function createUsersApi(authorizedSend: AuthorizedSend): UsersApi {
  // AI-generated (edited by James Teo): the transport and error handling moved
  // out of call() into request(), so the status route shares them without
  // mapping its empty 200 body onto a profile.
  async function request(
    path: string,
    init: { method?: "GET" | "PUT" | "PATCH"; body?: unknown } = {},
  ): Promise<HttpResponse> {
    let response;
    try {
      response = await authorizedSend({ baseUrl: config.gatewayBaseUrl, path, ...init });
    } catch (err) {
      if (err instanceof NetworkError) throw new UsersApiError(err.message, 0);
      throw err;
    }
    if (!response.ok) throw decodeError(response.body, response.status);
    return response;
  }

  async function call(
    uid: string,
    init: { method?: "GET" | "PUT"; body?: unknown } = {},
  ): Promise<UserProfile> {
    const response = await request(`${PREFIX}/${encodeURIComponent(uid)}`, init);
    return toUserProfile(response.body);
  }

  // AI-generated (edited by James Teo): the body is UpdateStatusRequest, whose
  // enum is ACTIVE and SUSPENDED only.
  async function setStatus(uid: string, status: "ACTIVE" | "SUSPENDED"): Promise<void> {
    await request(`${PREFIX}/${encodeURIComponent(uid)}/status`, {
      method: "PATCH",
      body: { status },
    });
  }

  function update(uid: string, body: Record<string, string>) {
    return call(uid, { method: "PUT", body });
  }

  return {
    getUser(uid) {
      return call(uid);
    },

    changeOwnUsername(uid, username) {
      return update(uid, { username: username.trim() });
    },

    changeOwnContact(uid, contact) {
      return update(uid, { phone_num: contact.trim() });
    },

    async changeOwnPassword(uid, currentPassword, newPassword) {
      try {
        return await update(uid, { currentPassword, newPassword });
      } catch (err) {
        // The authorized transport has already refreshed and retried once, and
        // a failed refresh ends the session. So a 401 that reaches here is
        // user-service's "invalid current password".
        if (err instanceof UsersApiError && err.status === 401) {
          throw new UsersApiError("Your current password is incorrect.", 401);
        }
        throw err;
      }
    },

    resetPassword(uid, newPassword) {
      return update(uid, { newPassword });
    },

    // AI-generated (edited by James Teo).
    suspendUser(uid) {
      return setStatus(uid, "SUSPENDED");
    },

    reactivateUser(uid) {
      return setStatus(uid, "ACTIVE");
    },
  };
}

/** Turns user-service's error body into a UsersApiError, keeping its message. */
function decodeError(body: unknown, status: number): UsersApiError {
  const message =
    body && typeof body === "object" && "error" in body
      ? String((body as { error: unknown }).error)
      : `Request failed (${status})`;
  return new UsersApiError(message, status);
}

/** Maps UserResponse or RestrictedUserResponse onto UserProfile. */
export function toUserProfile(body: unknown): UserProfile {
  const raw = (body ?? {}) as Record<string, unknown>;
  return {
    uid: String(raw["uid"] ?? ""),
    username: String(raw["username"] ?? ""),
    email: optionalText(raw["email"]),
    contact: optionalText(raw["phone_num"]),
    role: oneOf(raw["account_role"], ["STUDENT", "ADMIN"] as const),
    status: oneOf(raw["account_status"], ["ACTIVE", "SUSPENDED", "UNVERIFIED"] as const),
    createdAt: optionalText(raw["date_created"]),
  };
}

function optionalText(value: unknown): string | undefined {
  return typeof value === "string" && value.trim() ? value : undefined;
}

function oneOf<T extends string>(value: unknown, allowed: readonly T[]): T | undefined {
  return allowed.includes(value as T) ? (value as T) : undefined;
}
