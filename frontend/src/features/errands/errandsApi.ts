// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: MOCK client standing in for order-service.
// Author review: Nigeltzy - AI used to generate boilerplate implementation for mock client scaffolding and for testing. Checked and is valid and works as mock.

import { mockDelay, nextErrandId } from "../../lib/mock";
import type { Errand, NewErrand } from "./types";

/**
 * Mock client for order-service: an in-memory list, reset on every page
 * reload. It stores and returns errands and enforces no rule; acceptance,
 * expiry and state changes belong to order-service. EXPIRY_HOURS only fills
 * in `expiresAt` to match the two hours quoted in the UI.
 */

const EXPIRY_HOURS = 2;

const errands: Errand[] = [
  {
    id: "E-1039",
    status: "OPEN",
    supplierId: "mock-supplier-1",
    supplierName: "Coffee Bean @ COM3",
    items: [{ id: "i1", text: "1 × Iced latte, less ice" }],
    deliverTo: "COM1 Level 1 study benches",
    findMe: "Third table from the window, grey backpack",
    reward: 14,
    createdAt: "2026-09-08T19:06:00+08:00",
    expiresAt: "2026-09-08T21:06:00+08:00",
  },
  {
    id: "E-1036",
    status: "COMPLETED",
    supplierId: "mock-supplier-2",
    supplierName: "FairPrice @ UTown",
    items: [{ id: "i2", text: "1 × Pack of AA batteries" }],
    deliverTo: "UTown Residences lobby",
    findMe: "",
    reward: 10,
    createdAt: "2026-09-08T16:44:00+08:00",
    expiresAt: "2026-09-08T18:44:00+08:00",
  },
];

export function listErrands(): Promise<Errand[]> {
  return mockDelay([...errands]);
}

export function createErrand(input: NewErrand): Promise<Errand> {
  const now = new Date();
  const expires = new Date(now.getTime() + EXPIRY_HOURS * 3600 * 1000);
  const errand: Errand = {
    id: nextErrandId(),
    status: "OPEN",
    supplierId: input.supplierId,
    supplierName: input.supplierName,
    items: input.items.map((text, i) => ({ id: `i-${i}`, text })),
    deliverTo: input.deliverTo,
    findMe: input.findMe,
    reward: input.reward,
    createdAt: now.toISOString(),
    expiresAt: expires.toISOString(),
  };
  errands.unshift(errand);
  return mockDelay(errand);
}
