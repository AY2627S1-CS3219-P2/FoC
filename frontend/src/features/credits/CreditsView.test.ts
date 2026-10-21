// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-18
// Scope: Tests for the Credits screen's logic: the shell's figures for each
//   balance state, the account kept through a failed poll, Load more
//   skipping rows already shown, a refetched first page merged in front of
//   the rows Load more added, Load more racing a new transaction in each
//   order, the message for each failed history fetch, and the amount each
//   row shows. Moved to credit-service's cursor paging and per-operation
//   rows on 2026-10-21.
// Author review: James Teo — reviewed and verified.

import { describe, expect, it } from "vitest";
import {
  accountShown,
  balanceFigures,
  formatAmount,
  ledgerFailureMessage,
  mergeTransactions,
  nextPageRequest,
  rowFigure,
  withLatestPage,
  withNextPage,
  type Ledger,
} from "./CreditsView";
import type {
  AccountResponse,
  TransactionOperation,
  TransactionListResponse,
} from "./creditTypes";

function row(operationId: string, availableDelta = 10, reservedDelta = 0): TransactionOperation {
  return {
    operationId,
    errandId: null,
    reason: "REGISTRATION_ALLOCATION",
    availableDelta,
    reservedDelta,
    occurredAt: null,
    createdAt: "2026-10-18T09:00:00Z",
  };
}

const ACCOUNT: AccountResponse = {
  userId: "u-1",
  creditStatus: "READY",
  availableCredits: 90,
  reservedCredits: 10,
  updatedAt: "2026-10-18T09:00:00Z",
};

describe("balanceFigures", () => {
  it("gives the account's figures once it is known", () => {
    expect(balanceFigures({ status: "ready", account: ACCOUNT })).toEqual({ available: 90, held: 10 });
  });

  it.each(["loading", "none", "unavailable"] as const)(
    "shows no figure while the balance is %s",
    (status) => {
      expect(balanceFigures({ status })).toEqual({ available: null, held: null });
    },
  );
});

describe("accountShown", () => {
  const before = ACCOUNT;
  const now: AccountResponse = { ...before, availableCredits: 85, reservedCredits: 15 };

  it("draws the account credit-service just sent", () => {
    expect(accountShown({ status: "ready", account: now }, before)).toBe(now);
  });

  it("keeps the last account while credit-service is unavailable", () => {
    expect(accountShown({ status: "unavailable" }, before)).toBe(before);
  });

  it("draws nothing when credit-service has been unavailable since the screen opened", () => {
    expect(accountShown({ status: "unavailable" }, null)).toBeNull();
  });

  it.each(["loading", "none"] as const)("draws nothing while the balance is %s", (status) => {
    expect(accountShown({ status }, before)).toBeNull();
  });
});

describe("mergeTransactions", () => {
  it("appends a page in order", () => {
    expect(mergeTransactions([row("a"), row("b")], [row("c")]).map((r) => r.operationId)).toEqual([
      "a",
      "b",
      "c",
    ]);
  });

  it("skips rows already shown", () => {
    const shown = [row("a"), row("b")];
    const next = [row("b"), row("c"), row("d")];
    expect(mergeTransactions(shown, next).map((r) => r.operationId)).toEqual(["a", "b", "c", "d"]);
  });

  it("leaves the rows shown untouched", () => {
    const shown = [row("a")];
    mergeTransactions(shown, [row("b")]);
    expect(shown.map((r) => r.operationId)).toEqual(["a"]);
  });
});

describe("rowFigure", () => {
  it.each([
    ["starting credits", 100, 0, 100, false],
    ["a hold", -10, 10, -10, false],
    ["a raised reward", -5, 5, -5, false],
    ["a refund", 10, -10, 10, false],
    ["a courier's payout", 15, 0, 15, false],
    ["the requester's payment out of the hold", 0, -15, -15, true],
  ])("shows %s (%i available, %i held) as %i, held %s", (_name, available, held, amount, isHeld) => {
    expect(rowFigure(row("x", available, held))).toEqual({ amount, held: isHeld });
  });

  it("makes the unheld figures add up to the available balance", () => {
    const history = [row("a", 100, 0), row("b", -10, 10), row("c", 0, -10), row("d", -5, 5), row("e", 5, -5)];
    const sum = history.map(rowFigure).filter((f) => !f.held).reduce((n, f) => n + f.amount, 0);
    expect(sum).toBe(90);
  });
});

/** Ids "t<from>" down to "t<to>", newest first, as credit-service pages them. */
function rows(from: number, to: number): TransactionOperation[] {
  const out: TransactionOperation[] = [];
  for (let n = from; n >= to; n--) out.push(row(`t${n}`));
  return out;
}

function page(items: TransactionOperation[], nextCursor: string | null): TransactionListResponse {
  return { items, nextCursor };
}

const ids = (ledger: Ledger) => ledger.items.map((item) => item.operationId);

describe("withLatestPage", () => {
  it("starts from the page when nothing is shown yet", () => {
    const ledger = withLatestPage(null, page(rows(50, 31), "c31"));
    expect(ids(ledger)).toEqual(rows(50, 31).map((r) => r.operationId));
    expect(ledger.nextCursor).toBe("c31");
  });

  it("treats a page with no nextCursor as the last", () => {
    expect(withLatestPage(null, { items: rows(3, 1) }).nextCursor).toBeNull();
  });

  it("keeps the rows Load more added and their cursor, and puts new ones in front", () => {
    // Forty rows shown (t50..t11), then three transactions arrive.
    const shown: Ledger = { items: rows(50, 11), nextCursor: "c11" };
    const ledger = withLatestPage(shown, page(rows(53, 34), "c34"));
    expect(ids(ledger)).toEqual(rows(53, 11).map((r) => r.operationId));
    expect(ledger.nextCursor).toBe("c11");
  });

  it("changes nothing when no transaction is new", () => {
    const shown: Ledger = { items: rows(50, 11), nextCursor: "c11" };
    const ledger = withLatestPage(shown, page(rows(50, 31), "c31"));
    expect(ledger).toEqual(shown);
  });

  it("keeps a finished ledger finished when new rows arrive in front", () => {
    const shown: Ledger = { items: rows(20, 1), nextCursor: null };
    expect(withLatestPage(shown, page(rows(22, 3), "c3")).nextCursor).toBeNull();
  });

  it("starts again from the page when more than a page arrived", () => {
    const shown: Ledger = { items: rows(50, 11), nextCursor: "c11" };
    const ledger = withLatestPage(shown, page(rows(80, 61), "c61"));
    expect(ids(ledger)).toEqual(rows(80, 61).map((r) => r.operationId));
    expect(ledger.nextCursor).toBe("c61");
  });

  it("leaves the ledger it was given untouched", () => {
    const shown: Ledger = { items: rows(30, 11), nextCursor: "c11" };
    withLatestPage(shown, page(rows(31, 12), "c12"));
    expect(ids(shown)).toEqual(rows(30, 11).map((r) => r.operationId));
  });
});

describe("nextPageRequest", () => {
  it("asks with the cursor, after the last row shown", () => {
    expect(nextPageRequest({ items: rows(50, 31), nextCursor: "c31" })).toEqual({
      cursor: "c31",
      after: "t31",
    });
  });

  it("asks for nothing once credit-service sends no cursor", () => {
    expect(nextPageRequest({ items: rows(20, 1), nextCursor: null })).toBeNull();
  });
});

describe("withNextPage", () => {
  it("appends the page and takes its cursor", () => {
    const shown: Ledger = { items: rows(41, 22), nextCursor: "c22" };
    const ledger = withNextPage(shown, { cursor: "c22", after: "t22" }, page(rows(21, 2), "c2"));
    expect(ids(ledger)).toEqual(rows(41, 2).map((r) => r.operationId));
    expect(ledger.nextCursor).toBe("c2");
  });

  it("ends the ledger on the last page", () => {
    const shown: Ledger = { items: rows(25, 6), nextCursor: "c6" };
    const ledger = withNextPage(shown, { cursor: "c6", after: "t6" }, page(rows(5, 1), null));
    expect(nextPageRequest(ledger)).toBeNull();
  });

  it("drops the page when the ledger started again while it was on its way", () => {
    const restarted: Ledger = { items: rows(80, 61), nextCursor: "c61" };
    const ledger = withNextPage(restarted, { cursor: "c31", after: "t31" }, page(rows(30, 11), "c11"));
    expect(ledger).toBe(restarted);
  });
});

/**
 * credit-service's ledger, newest first, paged by a cursor that names the
 * last row of the previous page, as its keyset paging does. It answers a page
 * from whatever it holds when the request is served, which may be before or
 * after the answer reaches the screen.
 */
function ledgerService(count: number) {
  let newest = count;
  let held = rows(count, 1);
  return {
    /** A transaction lands at the newest end. */
    add() {
      newest += 1;
      held = [row(`t${newest}`), ...held];
    },
    serve(cursor?: string): TransactionListResponse {
      const start = cursor === undefined ? 0 : held.findIndex((r) => `c:${r.operationId}` === cursor) + 1;
      const items = held.slice(start, start + 20);
      const more = start + 20 < held.length;
      return { items, nextCursor: more ? `c:${items.at(-1)!.operationId}` : null };
    },
    ids: () => held.map((item) => item.operationId),
  };
}

/** Presses Load more, answered at once, until it goes away. */
function loadRest(ledger: Ledger, service: ReturnType<typeof ledgerService>): Ledger {
  for (let request = nextPageRequest(ledger); request; request = nextPageRequest(ledger)) {
    ledger = withNextPage(ledger, request, service.serve(request.cursor));
  }
  return ledger;
}

describe("Load more racing a new transaction", () => {
  // The screen shows the first page; Load more is pressed as a transaction
  // lands, and the balance poll then refetches the first page. The two
  // answers can be served on either side of the transaction and merged in
  // either order. 41 and 61 rows leave one more page after Load more's, so a
  // row skipped would show as a missing row.
  const orderings = [
    { servedBefore: true, moreFirst: false },
    { servedBefore: true, moreFirst: true },
    { servedBefore: false, moreFirst: false },
    { servedBefore: false, moreFirst: true },
  ];

  for (const count of [40, 41, 61]) {
    for (const { servedBefore, moreFirst } of orderings) {
      const name =
        `Load more served ${servedBefore ? "before" : "after"} the transaction, ` +
        `${moreFirst ? "Load more" : "the first page"} merged first (${count} rows)`;
      it(`${name}: every row once, newest first`, () => {
        const service = ledgerService(count);
        const shown = withLatestPage(null, service.serve());
        const request = nextPageRequest(shown)!;

        let morePage: TransactionListResponse;
        if (servedBefore) {
          morePage = service.serve(request.cursor);
          service.add();
        } else {
          service.add();
          morePage = service.serve(request.cursor);
        }
        const firstPage = service.serve();

        let ledger = shown;
        if (moreFirst) {
          ledger = withNextPage(ledger, request, morePage);
          ledger = withLatestPage(ledger, firstPage);
        } else {
          ledger = withLatestPage(ledger, firstPage);
          ledger = withNextPage(ledger, request, morePage);
        }
        ledger = loadRest(ledger, service);

        expect(ids(ledger)).toEqual(service.ids());
        expect(nextPageRequest(ledger)).toBeNull();
      });
    }
  }
});

describe("ledgerFailureMessage", () => {
  it("says the history did not load when no row is shown yet", () => {
    expect(ledgerFailureMessage("first", false)).toBe("Couldn’t load your history.");
  });

  it("says only the latest rows are missing when a refetch of the first page fails", () => {
    expect(ledgerFailureMessage("first", true)).toBe(
      "Couldn’t load your latest transactions.",
    );
  });

  it("says Load more failed, whatever is shown", () => {
    expect(ledgerFailureMessage("more", true)).toBe("Couldn’t load more history.");
  });
});

describe("formatAmount", () => {
  it("shows the sign credit-service sends", () => {
    expect(formatAmount(100)).toBe("+100");
    expect(formatAmount(-10)).toBe("-10");
    expect(formatAmount(0)).toBe("0");
  });
});
