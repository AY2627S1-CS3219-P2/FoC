// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-18
// Scope: Tests for the Credits screen's logic: the shell's figures for each
//   balance state, the account kept through a failed poll, Load more
//   skipping rows already shown and asking from the number of rows shown, a
//   refetched first page merged in front of the rows Load more added, Load
//   more racing a new transaction in each order, the message for each failed
//   history fetch, and amounts shown with the sign credit-service sends.
// Author review: James Teo — reviewed and verified.

import { describe, expect, it } from "vitest";
import {
  accountShown,
  balanceFigures,
  formatAmount,
  ledgerFailureMessage,
  mergeTransactions,
  nextPageRequest,
  withLatestPage,
  withNextPage,
  type Ledger,
} from "./CreditsView";
import type { AccountResponse, TransactionItem, TransactionListResponse } from "./creditTypes";

function row(id: string, amount = 10): TransactionItem {
  return {
    id,
    userId: "u-1",
    errandId: null,
    amount,
    reason: "REGISTRATION_ALLOCATION",
    createdAt: "2026-10-18T09:00:00Z",
  };
}

describe("balanceFigures", () => {
  it("gives the account's figures once it is known", () => {
    expect(
      balanceFigures({
        status: "ready",
        account: {
          userId: "u-1",
          availableCredits: 90,
          reservedCredits: 10,
          updatedAt: "2026-10-18T09:00:00Z",
        },
      }),
    ).toEqual({ available: 90, held: 10 });
  });

  it.each(["loading", "none", "unavailable"] as const)(
    "shows no figure while the balance is %s",
    (status) => {
      expect(balanceFigures({ status })).toEqual({ available: null, held: null });
    },
  );
});

describe("accountShown", () => {
  const before: AccountResponse = {
    userId: "u-1",
    availableCredits: 90,
    reservedCredits: 10,
    updatedAt: "2026-10-18T09:00:00Z",
  };
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
    expect(mergeTransactions([row("a"), row("b")], [row("c")]).map((r) => r.id)).toEqual([
      "a",
      "b",
      "c",
    ]);
  });

  it("skips rows already shown, as when a new transaction shifts the offsets", () => {
    const shown = [row("a"), row("b")];
    const next = [row("b"), row("c"), row("d")];
    expect(mergeTransactions(shown, next).map((r) => r.id)).toEqual(["a", "b", "c", "d"]);
  });

  it("leaves the rows shown untouched", () => {
    const shown = [row("a")];
    mergeTransactions(shown, [row("b")]);
    expect(shown.map((r) => r.id)).toEqual(["a"]);
  });
});

/** Ids "t<from>" down to "t<to>", newest first, as credit-service pages them. */
function rows(from: number, to: number): TransactionItem[] {
  const out: TransactionItem[] = [];
  for (let n = from; n >= to; n--) out.push(row(`t${n}`));
  return out;
}

function page(items: TransactionItem[], total: number): TransactionListResponse {
  return { items, pagination: { limit: 20, offset: 0, total } };
}

const ids = (ledger: Ledger) => ledger.items.map((item) => item.id);

describe("withLatestPage", () => {
  it("starts from the page when nothing is shown yet", () => {
    const ledger = withLatestPage(null, page(rows(50, 31), 50));
    expect(ids(ledger)).toEqual(rows(50, 31).map((r) => r.id));
    expect(ledger.total).toBe(50);
  });

  it("keeps the rows Load more added and puts new ones in front", () => {
    // Forty rows shown (t50..t11), then three transactions arrive.
    const shown: Ledger = { items: rows(50, 11), total: 50 };
    const ledger = withLatestPage(shown, page(rows(53, 34), 53));
    expect(ids(ledger)).toEqual(rows(53, 11).map((r) => r.id));
    expect(ledger.total).toBe(53);
  });

  it("changes nothing when no transaction is new", () => {
    const shown: Ledger = { items: rows(50, 11), total: 50 };
    const ledger = withLatestPage(shown, page(rows(50, 31), 50));
    expect(ledger).toEqual(shown);
  });

  it("keeps the larger total when a Load more page reported a newer one", () => {
    const shown: Ledger = { items: rows(51, 12), total: 51 };
    expect(withLatestPage(shown, page(rows(50, 31), 50)).total).toBe(51);
  });

  it("starts again from the page when more than a page arrived", () => {
    const shown: Ledger = { items: rows(50, 11), total: 50 };
    const ledger = withLatestPage(shown, page(rows(80, 61), 80));
    expect(ids(ledger)).toEqual(rows(80, 61).map((r) => r.id));
    expect(ledger.total).toBe(80);
  });

  it("leaves the ledger it was given untouched", () => {
    const shown: Ledger = { items: rows(30, 11), total: 30 };
    withLatestPage(shown, page(rows(31, 12), 31));
    expect(ids(shown)).toEqual(rows(30, 11).map((r) => r.id));
  });
});

describe("nextPageRequest", () => {
  it("asks from the number of rows shown, after the last one", () => {
    expect(nextPageRequest({ items: rows(50, 31), total: 50 })).toEqual({
      offset: 20,
      after: "t31",
    });
  });

  it("asks for nothing once every row is shown", () => {
    expect(nextPageRequest({ items: rows(20, 1), total: 20 })).toBeNull();
  });
});

describe("withNextPage", () => {
  it("appends the page and skips a row already shown", () => {
    const shown: Ledger = { items: rows(41, 22), total: 41 };
    const ledger = withNextPage(shown, { offset: 20, after: "t22" }, page(rows(22, 3), 41));
    expect(ids(ledger)).toEqual(rows(41, 3).map((r) => r.id));
  });

  it("keeps the larger total when the page was answered before a newer first page", () => {
    const shown: Ledger = { items: rows(42, 22), total: 42 };
    const ledger = withNextPage(shown, { offset: 20, after: "t22" }, page(rows(21, 2), 41));
    expect(ledger.total).toBe(42);
    expect(nextPageRequest(ledger)).toEqual({ offset: 41, after: "t2" });
  });

  it("drops the page when the ledger started again while it was on its way", () => {
    const restarted: Ledger = { items: rows(80, 61), total: 80 };
    const ledger = withNextPage(restarted, { offset: 20, after: "t31" }, page(rows(30, 11), 50));
    expect(ledger).toBe(restarted);
  });
});

/**
 * credit-service's ledger, newest first: it answers a page from whatever it
 * holds when the request is served, which may be before or after the answer
 * reaches the screen.
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
    serve(offset: number): TransactionListResponse {
      return {
        items: held.slice(offset, offset + 20),
        pagination: { limit: 20, offset, total: held.length },
      };
    },
    ids: () => held.map((item) => item.id),
  };
}

/** Presses Load more, answered at once, until it goes away. */
function loadRest(ledger: Ledger, service: ReturnType<typeof ledgerService>): Ledger {
  for (let request = nextPageRequest(ledger); request; request = nextPageRequest(ledger)) {
    ledger = withNextPage(ledger, request, service.serve(request.offset));
  }
  return ledger;
}

describe("Load more racing a new transaction", () => {
  // The screen shows the first page; Load more is pressed as a transaction
  // lands, and the balance poll then refetches the first page. The two
  // answers can be served on either side of the transaction and merged in
  // either order. 41 and 61 rows leave one more page after Load more's, so a
  // row skipped or a total gone stale would show as a missing row.
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
        const shown = withLatestPage(null, service.serve(0));
        const request = nextPageRequest(shown)!;

        let morePage: TransactionListResponse;
        if (servedBefore) {
          morePage = service.serve(request.offset);
          service.add();
        } else {
          service.add();
          morePage = service.serve(request.offset);
        }
        const firstPage = service.serve(0);

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
    expect(ledgerFailureMessage("first", false)).toBe("Couldn\u2019t load your history.");
  });

  it("says only the latest rows are missing when a refetch of the first page fails", () => {
    expect(ledgerFailureMessage("first", true)).toBe(
      "Couldn\u2019t load your latest transactions.",
    );
  });

  it("says Load more failed, whatever is shown", () => {
    expect(ledgerFailureMessage("more", true)).toBe("Couldn\u2019t load more history.");
  });
});

describe("formatAmount", () => {
  it("shows the sign credit-service sends", () => {
    expect(formatAmount(100)).toBe("+100");
    expect(formatAmount(-10)).toBe("-10");
    expect(formatAmount(0)).toBe("0");
  });
});
