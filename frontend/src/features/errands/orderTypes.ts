// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-18
// Scope: Types for order-service's errand API, hand-written by transcribing
//   order-service/api/openapi.yaml from PR #21 (feat/order-service-lifecycle,
//   commit 632c666): ErrandState, ErrandAction, HistoryAction,
//   SupplierSnapshot, Errand, FieldChange, HistoryEntry, ErrandDetail,
//   ErrandList, AssignmentRecord, AssignedList, LocationList,
//   CreateErrandRequest and AmendErrandRequest. Replaces the mock's types.ts.
// Author review: James Teo — reviewed and verified.

/**
 * The shapes order-service sends and accepts, field for field as its
 * api/openapi.yaml names them. Nothing here is invented: when the spec
 * changes, this file is re-transcribed rather than adapted. Field names are
 * kept as the wire sends them, so a response is used as it arrives.
 *
 * Which transitions are allowed, and so which buttons an errand offers, is
 * order-service's to decide: it sends them as `actions`, and the client never
 * works one out itself.
 */

/**
 * An errand's state, the spec's `ErrandState`. It is the order-state set the
 * team recorded in the milestone report's glossary. COMPLETED, CANCELLED and
 * EXPIRED are final. On the wire the field is called `state`.
 */
export const ERRAND_STATUSES = [
  "OPEN",
  "ACCEPTED",
  "PICKED_UP",
  "DELIVERED",
  "COMPLETED",
  "CANCELLED",
  "EXPIRED",
] as const;

export type ErrandStatus = (typeof ERRAND_STATUSES)[number];

/**
 * An action the viewer may take now. Each maps to the route of the same
 * name, `POST /api/orders/{id}/<action>`, except `amend`, which is
 * `PATCH /api/orders/{id}`. order-service lists them in this order.
 */
export const ERRAND_ACTIONS = [
  "amend",
  "accept",
  "withdraw",
  "pickup",
  "deliver",
  "confirm",
  "cancel",
  "complete",
] as const;

export type ErrandAction = (typeof ERRAND_ACTIONS)[number];

/** What one history entry records. */
export const HISTORY_ACTIONS = [
  "CREATE",
  "AMEND",
  "ACCEPT",
  "WITHDRAW",
  "PICK_UP",
  "DELIVER",
  "CONFIRM",
  "AUTO_CONFIRM",
  "CANCEL",
  "EXPIRE",
  "ADMIN_CANCEL",
  "ADMIN_COMPLETE",
] as const;

export type HistoryAction = (typeof HISTORY_ACTIONS)[number];

/** The supplier as it was when the errand was posted or last amended. */
export interface SupplierSnapshot {
  id: string;
  name: string;
  location: string;
}

export interface Errand {
  id: string;
  state: ErrandStatus;
  /** Send this back on amend and accept. */
  version: number;
  supplier: SupplierSnapshot;
  itemDescription: string;
  deliveryLocation: string;
  /** Present only for the requester, the assigned courier and ADMIN. */
  deliveryNote?: string;
  reward: number;
  /** Present only for the requester, the assigned courier and ADMIN. */
  requesterId?: string;
  /** Present while a courier is assigned. */
  courierId?: string;
  /** RFC 3339, UTC, as are all the times below. */
  createdAt: string;
  updatedAt: string;
  stateChangedAt: string;
  /** Present while OPEN (F3.2.3). */
  expiresAt?: string;
  /** Present while DELIVERED; the errand completes on its own after this (F3.4.2, F3.9.4). */
  confirmBy?: string;
  actions: ErrandAction[];
}

export type ChangedField =
  | "supplier"
  | "itemDescription"
  | "deliveryLocation"
  | "deliveryNote"
  | "reward";

/**
 * One field an amendment changed. `from` and `to` are text: the supplier's
 * name for a supplier change, the number as text for a reward change.
 */
export interface FieldChange {
  field: ChangedField;
  from: string;
  to: string;
}

export interface HistoryEntry {
  action: HistoryAction;
  fromState?: ErrandStatus;
  toState: ErrandStatus;
  occurredAt: string;
  /**
   * Present on AMEND entries (F3.7.7). A `deliveryNote` change is left out for
   * a viewer who may not see the note, and shown to the assigned courier with
   * empty `from` and `to`.
   */
  changes?: FieldChange[];
}

/** One errand, its history, and the server's clock when it answered. */
export interface ErrandDetail {
  errand: Errand;
  history: HistoryEntry[];
  /** Countdowns are worked out against this, not the device's clock. */
  serverTime: string;
}

export interface ErrandList {
  items: Errand[];
  serverTime: string;
}

/** The `scope` of the requester's and courier's lists. */
export type ListScope = "active" | "history";

export type AssignmentOutcome = "COMPLETED" | "CANCELLED" | "RELEASED";

/**
 * One ended assignment: how and when it ended, with the errand's current
 * supplier, item, location and reward. A RELEASED one is an errand the
 * courier withdrew from, which they can no longer open.
 */
export interface AssignmentRecord {
  errandId: string;
  outcome: AssignmentOutcome;
  endedAt: string;
  supplier: SupplierSnapshot;
  itemDescription: string;
  deliveryLocation: string;
  reward: number;
}

/**
 * The courier's list. `items` is filled for `scope=active`, `history` for
 * `scope=history`.
 */
export interface AssignedList {
  items: Errand[];
  history: AssignmentRecord[];
  serverTime: string;
}

/** The body of `POST /api/orders`. */
/** One entry of `GET /locations`: what an errand stores, and the group the form lists it under. */
export interface CampusLocation {
  name: string;
  area: string;
}

export interface CreateErrandRequest {
  supplierId: string;
  /** 1–280 characters, at least one visible; no line breaks. */
  itemDescription: string;
  /** One of `GET /api/orders/locations`. */
  deliveryLocation: string;
  /** At most 140 characters; line breaks allowed. */
  deliveryNote?: string;
  /** A whole number, at least 1. */
  reward: number;
}

/**
 * The body of `PATCH /api/orders/{id}`: `version` plus at least one field to
 * change. An empty `deliveryNote` clears the note.
 */
export interface AmendErrandRequest {
  version: number;
  supplierId?: string;
  itemDescription?: string;
  deliveryLocation?: string;
  deliveryNote?: string;
  reward?: number;
}

/** The fields an amendment may change, without the version it is sent with. */
export type ErrandChanges = Omit<AmendErrandRequest, "version">;
