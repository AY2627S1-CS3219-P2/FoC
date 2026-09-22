// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: MOCK client standing in for order-service.
// Author review: Nigeltzy - AI used to generate boilerplate implementation for mock client scaffolding and for testing. Checked and is valid and works as mock.

import { mockDelay, nextErrandId } from "../../lib/mock";
import type { Errand, NewErrand } from "./types";

/**
 * ███ MOCK — `order-service` does not exist (M4 is unimplemented). ███
 *
 * An in-memory array, reset on every page reload. It decides nothing: not
 * whether an errand may be accepted, not who may accept it, not what happens
 * when one expires. order-service/AGENTS.md lists all of that as its owner's
 * decisions, and frontend/AGENTS.md forbids the frontend re-implementing a
 * rule a service owns — so this fixture stores and returns, nothing more.
 *
 * The two-hour expiry quoted in the UI is copy from the owner's mockup, not a
 * rule enforced here.
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
