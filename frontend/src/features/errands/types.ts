// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Placeholder types for the mock order-service.
// Author review: Nigeltzy - AI used to generate the config and assign the types based on our team's planned architecture design (most types are intuitive and decided by me)

/**
 * `order-service` does not exist yet, so the rest of this file is still a MOCK
 * shape invented by the frontend (see src/lib/mock.ts) — EXCEPT ErrandStatus,
 * which transcribes a decision the team has already recorded.
 *
 * Vocabulary (root AGENTS.md): an ERRAND is what a user asks for; the record
 * order-service stores for it is an ORDER; the two people on one are the
 * REQUESTER and the COURIER.
 */

/**
 * The order state set, transcribed from the glossary in the team's milestone
 * report: an errand request is
 * in exactly one of these states, with CANCELLED and EXPIRED as terminal
 * alternatives.
 *
 * Which transitions are permitted is governed by requirement F3.8 and enforced
 * by order-service — never here. The frontend renders the state it is given and
 * reports what the service refuses (frontend/AGENTS.md).
 *
 * Casing follows the glossary. Whether the wire format carries "OPEN" or "open"
 * is order-service's serialisation decision; adjust when its spec lands.
 */
export type ErrandStatus =
  | "OPEN"
  | "ACCEPTED"
  | "PICKED_UP"
  | "DELIVERED"
  | "COMPLETED"
  | "CANCELLED"
  | "EXPIRED";

/** The two terminal alternatives named in the glossary. */
export const TERMINAL_STATUSES: ErrandStatus[] = ["CANCELLED", "EXPIRED"];

export interface ErrandItem {
  id: string;
  /** Free text as typed, e.g. "1 × Iced latte, less ice". */
  text: string;
}

export interface Errand {
  id: string;
  status: ErrandStatus;
  /** Supplier IDs only — never a copy of the supplier record (root §5). */
  supplierId: string;
  /** Resolved at the edge for display; not stored on the order. */
  supplierName: string;
  items: ErrandItem[];
  deliverTo: string;
  findMe: string;
  reward: number;
  createdAt: string;
  expiresAt: string;
}

export interface NewErrand {
  supplierId: string;
  supplierName: string;
  items: string[];
  deliverTo: string;
  findMe: string;
  reward: number;
}
