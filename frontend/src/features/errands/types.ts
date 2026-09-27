// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Placeholder types for the mock order-service.
// Author review: Nigeltzy - AI used to generate the config and assign the types based on our team's planned architecture design (most types are intuitive and decided by me)

/**
 * Fixture shapes for the mock order-service, not a contract. ErrandStatus is
 * the exception: it transcribes the state set the team recorded.
 */

/**
 * The order state set from the glossary in the team's milestone report, in
 * the glossary's casing. CANCELLED and EXPIRED are the terminal alternatives.
 * Which transitions are permitted (F3.8) is enforced by order-service, never
 * here; the frontend renders the state it is given.
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
  /** Copied from the chosen supplier when the errand is posted, for display. */
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
