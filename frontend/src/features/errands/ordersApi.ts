// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-18
// Scope: Client for order-service's errand routes through the gateway, one
//   function per route, hand-written by transcribing
//   order-service/api/openapi.yaml from PR #21 (feat/order-service-lifecycle,
//   commit 632c666): lists, detail, create with its X-Request-Id key, amend
//   and the seven actions. Replaces the mock errandsApi.ts.
// Author review: James Teo — reviewed and verified.

import type { AuthorizedSend } from "../auth/session";
import { config } from "../../lib/config";
import { NetworkError, queryString, type HttpResponse } from "../../lib/http";
import type {
  AssignedList,
  CampusLocation,
  CreateErrandRequest,
  Errand,
  ErrandChanges,
  ErrandDetail,
  ErrandList,
  HistoryEntry,
  ListScope,
} from "./orderTypes";

/**
 * The only module that calls order-service. Every call goes to the gateway
 * under PREFIX with the bearer token attached, and order-service checks the
 * token itself. Components never call send() or fetch() for an errand.
 *
 * Hand-written rather than generated, the same way usersApi.ts is written
 * against user-service's spec. When order-service's spec changes, this file
 * and orderTypes.ts are re-checked against it.
 */

/** The gateway's public prefix for order-service; the gateway strips it. */
const PREFIX = "/api/orders";

/**
 * An error from an order-service call. order-service answers
 * `{"error": "..."}` with a non-2xx status, and its messages are written for
 * users. `status` is 0 when no usable response arrived: the request never got
 * through, or a 2xx came back without the body the spec promises.
 *
 * features/errands/format.ts turns one into the text a screen shows.
 */
export class OrdersApiError extends Error {
  readonly status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = "OrdersApiError";
    this.status = status;
  }
}

/**
 * The surface a generated OpenAPI client would expose for these routes. The
 * actions are seven named functions rather than one taking the action's name,
 * so no caller passes a value that picks the branch.
 */
export interface OrdersApi {
  /** The campus delivery locations an errand may name, each area's together. */
  listLocations(): Promise<CampusLocation[]>;
  /** OPEN errands before their deadline, newest first (F3.2). */
  listOpen(): Promise<ErrandList>;
  /** The caller's own errands (F3.4). */
  listRequested(scope: ListScope): Promise<ErrandList>;
  /** The caller's assignments as courier: `items` for active, `history` for history (F3.5). */
  listAssigned(scope: ListScope): Promise<AssignedList>;
  /** Every errand that is not final, oldest state change first. ADMIN only. */
  listActiveForAdmin(): Promise<ErrandList>;
  getErrand(id: string): Promise<ErrandDetail>;
  /**
   * Posts an errand (F3.1). `requestId` is the duplicate-submission key
   * (F7.2): mint one per submission with newRequestId() and send the same one
   * on every retry of it. A key order-service has already seen answers with
   * the errand it created rather than posting a second one.
   */
  createErrand(input: CreateErrandRequest, requestId: string): Promise<ErrandDetail>;
  /**
   * Amends an OPEN errand (F3.7). `version` is the one on the newest copy the
   * screen holds; a stale one is refused with 409. Send only the fields that
   * change; an empty `deliveryNote` clears the note.
   */
  amendErrand(id: string, version: number, changes: ErrandChanges): Promise<ErrandDetail>;
  /** Accepts as courier (F3.3), with the version the screen was showing. */
  accept(id: string, version: number): Promise<ErrandDetail>;
  withdraw(id: string): Promise<ErrandDetail>;
  pickUp(id: string): Promise<ErrandDetail>;
  deliver(id: string): Promise<ErrandDetail>;
  confirm(id: string): Promise<ErrandDetail>;
  cancel(id: string): Promise<ErrandDetail>;
  complete(id: string): Promise<ErrandDetail>;
}

/** The fields of CreateErrandRequest, the only ones a create body carries. */
const CREATE_FIELDS = [
  "supplierId",
  "itemDescription",
  "deliveryLocation",
  "deliveryNote",
  "reward",
] as const satisfies readonly (keyof CreateErrandRequest)[];

/** The fields of AmendErrandRequest besides `version`. */
const AMEND_FIELDS = [
  "supplierId",
  "itemDescription",
  "deliveryLocation",
  "deliveryNote",
  "reward",
] as const satisfies readonly (keyof ErrandChanges)[];

/**
 * Builds the client over an authorized transport. App.tsx creates it once per
 * token store, alongside the other clients.
 *
 * Request bodies carry only the contract's fields: order-service refuses an
 * unknown field with 400, so anything else a caller passes is dropped here.
 */
export function createOrdersApi(authorizedSend: AuthorizedSend): OrdersApi {
  async function call(
    path: string,
    init: {
      method?: "GET" | "POST" | "PATCH";
      body?: unknown;
      headers?: Record<string, string>;
    } = {},
  ): Promise<unknown> {
    let response: HttpResponse;
    try {
      response = await authorizedSend({
        baseUrl: config.gatewayBaseUrl,
        path,
        ...init,
      });
    } catch (err) {
      if (err instanceof NetworkError) throw new OrdersApiError(err.message, 0);
      throw err;
    }
    if (!response.ok) throw decodeError(response.body, response.status);
    return response.body;
  }

  function errandPath(id: string): string {
    return `${PREFIX}/${encodeURIComponent(id)}`;
  }

  /** POST /api/orders/{id}/<route>, answered with the errand after the action. */
  async function act(id: string, route: string, body?: unknown): Promise<ErrandDetail> {
    return toErrandDetail(
      await call(`${errandPath(id)}/${route}`, { method: "POST", body }),
    );
  }

  return {
    async listLocations() {
      const body = await call(`${PREFIX}/locations`);
      if (!isRecord(body) || !isCampusLocations(body["items"])) throw unusableResponse();
      return body["items"];
    },

    async listOpen() {
      return toErrandList(await call(`${PREFIX}/open`));
    },

    async listRequested(scope) {
      return toErrandList(await call(`${PREFIX}/requested${queryString({ scope })}`));
    },

    async listAssigned(scope) {
      return toAssignedList(await call(`${PREFIX}/assigned${queryString({ scope })}`));
    },

    async listActiveForAdmin() {
      return toErrandList(await call(`${PREFIX}${queryString({ scope: "active" })}`));
    },

    async getErrand(id) {
      return toErrandDetail(await call(errandPath(id)));
    },

    async createErrand(input, requestId) {
      return toErrandDetail(
        await call(PREFIX, {
          method: "POST",
          body: pickDefined(input, CREATE_FIELDS),
          headers: { "X-Request-Id": requestId },
        }),
      );
    },

    async amendErrand(id, version, changes) {
      return toErrandDetail(
        await call(errandPath(id), {
          method: "PATCH",
          body: { version, ...pickDefined(changes, AMEND_FIELDS) },
        }),
      );
    },

    accept(id, version) {
      return act(id, "accept", { version });
    },

    withdraw(id) {
      return act(id, "withdraw");
    },

    pickUp(id) {
      return act(id, "pickup");
    },

    deliver(id) {
      return act(id, "deliver");
    },

    confirm(id) {
      return act(id, "confirm");
    },

    cancel(id) {
      return act(id, "cancel");
    },

    complete(id) {
      return act(id, "complete");
    },
  };
}

/**
 * A fresh duplicate-submission key for one create (F7.2): a random UUID, well
 * inside the spec's 64-character limit on X-Request-Id.
 *
 * crypto.randomUUID needs a secure context (HTTPS or localhost). Over plain
 * HTTP on another host it is missing, so the same kind of UUID is built from
 * crypto.getRandomValues, which works everywhere.
 */
export function newRequestId(): string {
  const { crypto } = globalThis;
  if (typeof crypto.randomUUID === "function") return crypto.randomUUID();

  const bytes = crypto.getRandomValues(new Uint8Array(16));
  // RFC 9562 version 4 and variant bits, as randomUUID sets them.
  bytes[6] = (bytes[6]! & 0x0f) | 0x40;
  bytes[8] = (bytes[8]! & 0x3f) | 0x80;
  const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

/** Turns order-service's error body into an OrdersApiError, keeping its message. */
function decodeError(body: unknown, status: number): OrdersApiError {
  const message =
    isRecord(body) && "error" in body
      ? String(body["error"])
      : `Request failed (${status})`;
  return new OrdersApiError(message, status);
}

/**
 * A 2xx whose body is not what the spec promises. Treated like a lost
 * response (status 0), since nothing usable came back.
 */
function unusableResponse(): OrdersApiError {
  return new OrdersApiError("No usable response from order-service", 0);
}

/** Copies the listed keys whose values are defined, and nothing else. */
function pickDefined<T extends object, K extends keyof T>(
  source: T,
  keys: readonly K[],
): Partial<Pick<T, K>> {
  const out: Partial<Pick<T, K>> = {};
  for (const key of keys) {
    if (source[key] !== undefined) out[key] = source[key];
  }
  return out;
}

function toErrandDetail(body: unknown): ErrandDetail {
  if (!isRecord(body) || !isErrand(body["errand"]) || typeof body["serverTime"] !== "string") {
    throw unusableResponse();
  }
  const history = body["history"];
  return {
    errand: body["errand"],
    history: Array.isArray(history) ? (history as HistoryEntry[]) : [],
    serverTime: body["serverTime"],
  };
}

function toErrandList(body: unknown): ErrandList {
  if (!isRecord(body) || !isErrandArray(body["items"]) || typeof body["serverTime"] !== "string") {
    throw unusableResponse();
  }
  return { items: body["items"], serverTime: body["serverTime"] };
}

function toAssignedList(body: unknown): AssignedList {
  if (
    !isRecord(body) ||
    !isErrandArray(body["items"]) ||
    !Array.isArray(body["history"]) ||
    typeof body["serverTime"] !== "string"
  ) {
    throw unusableResponse();
  }
  return {
    items: body["items"],
    history: body["history"] as AssignedList["history"],
    serverTime: body["serverTime"],
  };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isCampusLocations(value: unknown): value is CampusLocation[] {
  return (
    Array.isArray(value) &&
    value.every((v) => isRecord(v) && typeof v["name"] === "string" && typeof v["area"] === "string")
  );
}

/**
 * Checks the fields a screen reads without a guard (the id, state, version,
 * supplier and actions), so a malformed errand fails here rather than as a
 * crash in a component.
 */
function isErrand(value: unknown): value is Errand {
  return (
    isRecord(value) &&
    typeof value["id"] === "string" &&
    typeof value["state"] === "string" &&
    typeof value["version"] === "number" &&
    isRecord(value["supplier"]) &&
    Array.isArray(value["actions"])
  );
}

function isErrandArray(value: unknown): value is Errand[] {
  return Array.isArray(value) && value.every(isErrand);
}
