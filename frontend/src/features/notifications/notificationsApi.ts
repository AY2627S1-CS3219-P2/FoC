// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-05
// Scope: Client for notification-service through the gateway, hand-written by
//   transcribing notification-service/api/openapi.yaml (D-144).
// Author review: PENDING — <reviewer to complete>

import type { AuthorizedSend } from "../auth/session";
import { config } from "../../lib/config";
import { NetworkError, queryString, type HttpResponse } from "../../lib/http";

/** The gateway's public prefix for notification-service (D-144). */
const PREFIX = "/api/notifications";

/** One notification, as notification-service sends it. */
export interface Notification {
  id: string;
  errandId: string;
  /** The event that produced it, such as `errand.accepted`. */
  stateChange: string;
  /** The text to show the user. */
  message: string;
  read: boolean;
  createdAt: string;
}

/** One page of the signed-in user's notifications, newest first. */
export interface NotificationPage {
  notifications: Notification[];
  /** How many notifications the user has in all. */
  total: number;
}

/** An error from a notification-service call. `status` is 0 when no usable response arrived. */
export class NotificationsApiError extends Error {
  readonly status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = "NotificationsApiError";
    this.status = status;
  }
}

/** The surface a generated OpenAPI client would expose for these routes. */
export interface NotificationsApi {
  /** One page of the signed-in user's notifications. `limit` is 1–100. */
  list(limit: number, offset: number): Promise<NotificationPage>;
  /** Marks one of the signed-in user's notifications as read. */
  markRead(id: string): Promise<void>;
}

/** Builds the client over an authorized transport. */
export function createNotificationsApi(authorizedSend: AuthorizedSend): NotificationsApi {
  async function call(path: string, method: "GET" | "POST"): Promise<HttpResponse> {
    try {
      return await authorizedSend({ baseUrl: config.gatewayBaseUrl, path, method });
    } catch (err) {
      if (err instanceof NetworkError) throw new NotificationsApiError(err.message, 0);
      throw err;
    }
  }

  return {
    async list(limit, offset) {
      const query = queryString({ limit: String(limit), offset: String(offset) });
      const response = await call(`${PREFIX}${query}`, "GET");
      if (!response.ok) throw decodeError(response.body, response.status);
      if (!isPage(response.body)) {
        throw new NotificationsApiError("No usable response from notification-service", 0);
      }
      return response.body;
    },

    async markRead(id) {
      const response = await call(`${PREFIX}/${encodeURIComponent(id)}/read`, "POST");
      if (!response.ok) throw decodeError(response.body, response.status);
    },
  };
}

function decodeError(body: unknown, status: number): NotificationsApiError {
  const message =
    isRecord(body) && typeof body["error"] === "string"
      ? body["error"]
      : `Request failed (${status})`;
  return new NotificationsApiError(message, status);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isNotification(value: unknown): value is Notification {
  return (
    isRecord(value) &&
    typeof value["id"] === "string" &&
    typeof value["errandId"] === "string" &&
    typeof value["stateChange"] === "string" &&
    typeof value["message"] === "string" &&
    typeof value["read"] === "boolean" &&
    typeof value["createdAt"] === "string"
  );
}

function isPage(value: unknown): value is NotificationPage {
  return (
    isRecord(value) &&
    typeof value["total"] === "number" &&
    Array.isArray(value["notifications"]) &&
    value["notifications"].every(isNotification)
  );
}
