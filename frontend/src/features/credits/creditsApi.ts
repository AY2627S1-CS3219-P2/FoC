// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: MOCK client standing in for credit-service, seeded with the ledger
//   from the owner's mockup.
// Author review: Nigeltzy - AI used to create implementation for the mock client scaffolding and for testing. Checked and is valid and works as mock.

import { mockDelay } from "../../lib/mock";
import type { Balance, LedgerEntry } from "./types";

/**
 * Mock client for credit-service, seeded from the Credits mockup. It returns
 * the balance and ledger below and enforces nothing, so it is not a ledger.
 * Replace this file with credit-service's generated client.
 */

const LEDGER: LedgerEntry[] = [
  {
    id: "L-6",
    kind: "reservation",
    label: "Reserved for errand E-1039",
    amount: -14,
    runningAvailable: 76,
    occurredAt: "2026-09-08T19:06:00+08:00",
  },
  {
    id: "L-5",
    kind: "payment",
    label: "Paid to Goh Chee Yang for errand E-1036",
    amount: -10,
    runningAvailable: 90,
    occurredAt: "2026-09-08T17:08:00+08:00",
    note: "taken from held credits",
  },
  {
    id: "L-4",
    kind: "reservation",
    label: "Reserved for errand E-1036",
    amount: -10,
    runningAvailable: 90,
    occurredAt: "2026-09-08T16:44:00+08:00",
  },
  {
    id: "L-3",
    kind: "release",
    label: "Released — errand E-1031 expired",
    amount: +9,
    runningAvailable: 100,
    occurredAt: "2026-09-08T14:44:00+08:00",
  },
  {
    id: "L-2",
    kind: "reservation",
    label: "Reserved for errand E-1031",
    amount: -9,
    runningAvailable: 91,
    occurredAt: "2026-09-08T12:44:00+08:00",
  },
  {
    id: "L-1",
    kind: "allocation",
    label: "Initial allocation on sign-up",
    amount: +100,
    runningAvailable: 100,
    occurredAt: "2026-09-02T19:44:00+08:00",
  },
];

const BALANCE: Balance = { available: 76, held: 14 };

export function getBalance(): Promise<Balance> {
  return mockDelay({ ...BALANCE });
}

export function listLedger(): Promise<LedgerEntry[]> {
  return mockDelay([...LEDGER]);
}
