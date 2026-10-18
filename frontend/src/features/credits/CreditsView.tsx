// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Built the Credits screen from the owner's mockup, against the mock
//   credit-service.
// Author review: Nigeltzy - AI built it according to the prototype our team designed and the requirements provided. The code is fairly boilerplate, seems valid and works as intended.

import { useEffect, useState } from "react";
// AI-generated (edited by James Teo): credit-service's client and types replace
// the mock's; the balance comes from App, which polls it for the shell too.
import type { CreditAccountsApi } from "./creditAccountsApi";
import type {
  AccountResponse,
  TransactionItem,
  TransactionListResponse,
  TransactionReason,
} from "./creditTypes";

// AI-generated (edited by James Teo): the balance App holds, its figures, the
// account this screen draws, the ledger's paging and its amounts.

/**
 * The signed-in student's balance as App last heard it from credit-service:
 * still loading, no account yet (a 404 with the spec's error body),
 * unavailable (any other failure), or the account.
 */
export type Balance =
  | { status: "loading" }
  | { status: "none" }
  | { status: "unavailable" }
  | { status: "ready"; account: AccountResponse };

/** The shell's two figures; null shows "—", so no figure is ever made up. */
export function balanceFigures(balance: Balance): {
  available: number | null;
  held: number | null;
} {
  return balance.status === "ready"
    ? { available: balance.account.availableCredits, held: balance.account.reservedCredits }
    : { available: null, held: null };
}

/**
 * The account the Credits screen draws: the current one, or while
 * credit-service is unavailable the last one it sent, so one failed poll does
 * not take the balance and history off the screen. Null with no account.
 */
export function accountShown(
  balance: Balance,
  lastKnown: AccountResponse | null,
): AccountResponse | null {
  if (balance.status === "ready") return balance.account;
  return balance.status === "unavailable" ? lastKnown : null;
}

/**
 * Appends a page of transactions, skipping any already shown. A transaction
 * that lands between two pages shifts every offset by one, so without this a
 * row could appear twice.
 */
export function mergeTransactions(
  shown: readonly TransactionItem[],
  page: readonly TransactionItem[],
): TransactionItem[] {
  const seen = new Set(shown.map((item) => item.id));
  return [...shown, ...page.filter((item) => !seen.has(item.id))];
}

/**
 * An amount with the sign credit-service sends: "+5" for a credit, "-5" for
 * a debit. No sign is worked out here; a "+" is only written in front of a
 * positive number.
 */
export function formatAmount(amount: number): string {
  return amount > 0 ? `+${amount}` : String(amount);
}

// AI-generated (edited by James Teo).
interface CreditsViewProps {
  /** credit-service's client, built by App over the authorized transport. */
  api: CreditAccountsApi;
  /** The balance App polls, so this screen and the shell always agree. */
  balance: Balance;
  /** Asks App to fetch the balance again now. */
  onRetryBalance: () => void;
}

// AI-generated (edited by James Teo): one label per transaction reason, in place
// of the mock's free-text labels.
const REASON_LABEL: Record<TransactionReason, string> = {
  REGISTRATION_ALLOCATION: "Starting credits",
  ERRAND_RESERVATION: "Held for an errand",
  ERRAND_CREATION_FAILED: "Returned: errand could not be posted",
  ERRAND_COMPLETED_PAYOUT: "Earned for a delivery",
  ERRAND_COMPLETED_DEDUCTION: "Paid to courier",
  ERRAND_CANCELLED: "Returned: errand cancelled",
  ERRAND_EXPIRED: "Returned: errand expired",
  REWARD_INCREASED: "Extra held: reward raised",
  REWARD_DECREASED: "Returned: reward lowered",
};

// AI-generated (edited by James Teo): history is read 20 rows at a time.
const PAGE_SIZE = 20;

// AI-generated (edited by James Teo): the ledger as loaded so far, and where
// Load more asks from next.
export interface Ledger {
  /**
   * The rows shown, newest first. Each merge below keeps them a run of
   * credit-service's list with no row missing in between, which is what lets
   * the row count serve as the next page's offset.
   */
  items: TransactionItem[];
  /** The largest total credit-service has reported; the ledger only grows. */
  total: number;
}

/** A Load more fetch: where it starts, and the row shown last when it was asked for. */
export interface PageRequest {
  offset: number;
  after: string | undefined;
}

/**
 * What Load more asks for next, or null once every row is shown. The offset
 * is the number of rows shown rather than a running count of offsets asked
 * for: a new transaction lands at the newest end and pushes every row down
 * one, so asking from the row count can fetch a row again, which
 * mergeTransactions drops, but never steps over one.
 */
export function nextPageRequest(ledger: Ledger): PageRequest | null {
  const shown = ledger.items.length;
  if (shown >= ledger.total) return null;
  return { offset: shown, after: ledger.items[shown - 1]?.id };
}

/**
 * The ledger after Load more's page arrives, whenever it arrives relative to
 * a refetch of the first page. The page is dropped when the row it was asked
 * to follow is no longer shown: the ledger started again from a newer first
 * page while it was on its way, and appending it would leave a gap.
 */
export function withNextPage(
  current: Ledger,
  request: PageRequest,
  page: TransactionListResponse,
): Ledger {
  if (request.after !== undefined && !current.items.some((item) => item.id === request.after)) {
    return current;
  }
  return {
    items: mergeTransactions(current.items, page.items),
    // A page answered before a newer one can arrive after it, so its total
    // may be behind; the larger one is the newer.
    total: Math.max(current.total, page.pagination.total),
  };
}

// AI-generated (edited by James Teo): which history fetch failed, so Retry
// repeats that one and the message says which.
/**
 * The history fetch that failed: the first page, fetched again on every
 * balance change, or the next page Load more asked for.
 */
export type LedgerFailure = "first" | "more";

/** The message for a failed history fetch, given whether rows are shown. */
export function ledgerFailureMessage(failed: LedgerFailure, rowsShown: boolean): string {
  if (failed === "more") return "Couldn’t load more history.";
  return rowsShown
    ? "Couldn’t load your latest transactions."
    : "Couldn’t load your history.";
}

// AI-generated (edited by James Teo): a balance change refetches the first
// page without losing the rows Load more already added.
/**
 * The ledger after fetching its first page again. The newest transactions
 * come first, so any that arrived since are put in front of the rows already
 * shown. If the page shares no row with those shown, more than a page arrived
 * and the rows in between are unknown, so the ledger starts again from this
 * page.
 */
export function withLatestPage(current: Ledger | null, page: TransactionListResponse): Ledger {
  const fresh: Ledger = {
    items: mergeTransactions([], page.items),
    total: page.pagination.total,
  };
  if (!current || current.items.length === 0) return fresh;
  const shown = new Set(current.items.map((item) => item.id));
  if (!page.items.some((item) => shown.has(item.id))) return fresh;
  return {
    items: mergeTransactions(page.items, current.items),
    total: Math.max(current.total, page.pagination.total),
  };
}

export function CreditsView({ api, balance, onRetryBalance }: CreditsViewProps) {
  // AI-generated (edited by James Teo): the history from credit-service, its
  // first page fetched again whenever the account changes and merged in front
  // of the rows shown, with Load more and Retry; and the last account kept
  // through a failed balance poll.
  const [ledger, setLedger] = useState<Ledger | null>(null);
  const [ledgerFailed, setLedgerFailed] = useState<LedgerFailure | null>(null);
  const [loadingMore, setLoadingMore] = useState(false);
  const [ledgerAttempt, setLedgerAttempt] = useState(0);
  // The last account credit-service sent, kept through a failed poll.
  const [lastKnown, setLastKnown] = useState<AccountResponse | null>(null);
  if (balance.status === "ready" && balance.account !== lastKnown) {
    setLastKnown(balance.account);
  }
  const account = accountShown(balance, lastKnown);
  // Changes whenever credit-service reports a different account, which is when
  // the first page of history is fetched again.
  const accountVersion = account
    ? `${account.updatedAt}:${account.availableCredits}:${account.reservedCredits}`
    : null;

  useEffect(() => {
    if (accountVersion === null) return;
    let cancelled = false;
    setLedgerFailed(null);
    api.listTransactions(PAGE_SIZE, 0).then(
      (page) => {
        if (!cancelled) setLedger((current) => withLatestPage(current, page));
      },
      () => {
        if (!cancelled) setLedgerFailed("first");
      },
    );
    return () => {
      cancelled = true;
    };
  }, [api, accountVersion, ledgerAttempt]);

  async function loadMore() {
    const request = ledger && nextPageRequest(ledger);
    if (!request) return;
    setLoadingMore(true);
    setLedgerFailed(null);
    try {
      const page = await api.listTransactions(PAGE_SIZE, request.offset);
      setLedger((current) => (current ? withNextPage(current, request, page) : current));
    } catch {
      setLedgerFailed("more");
    } finally {
      setLoadingMore(false);
    }
  }

  // AI-generated (edited by James Teo): credit-service's field names.
  const total = account ? account.availableCredits + account.reservedCredits : 0;
  const availablePct = account && total ? (account.availableCredits / total) * 100 : 0;
  const hasMore = ledger !== null && nextPageRequest(ledger) !== null;

  return (
    <section>
      <div className="page-head">
        <h1>Credits</h1>
        <p className="page-sub">
          Credits only move between students on this platform. They can&rsquo;t
          be bought, cashed out, or spent with a shop.
        </p>
      </div>

      {/* AI-generated (edited by James Teo): loading, unavailable and
          no-account states, each with Retry. While credit-service is
          unavailable, the last balance and history it sent stay below the
          notice. */}
      {balance.status === "loading" && (
        <p className="status" role="status">
          Loading your balance…
        </p>
      )}

      {balance.status === "unavailable" && (
        <p className="status error" role="alert">
          Credits are unavailable right now
          {account ? ", so the figures below may be out of date" : ""}. Try again in a
          moment.{" "}
          <button type="button" className="link-btn" onClick={onRetryBalance}>
            Retry
          </button>
        </p>
      )}

      {balance.status === "none" && (
        <p className="status" role="status">
          Your credit account isn&rsquo;t open yet. It opens a few seconds after
          you verify your email.{" "}
          <button type="button" className="link-btn" onClick={onRetryBalance}>
            Retry
          </button>
        </p>
      )}

      {account && (
        <>
          {/* AI-generated (edited by James Teo): the balance from
              availableCredits and reservedCredits. */}
          <div className="card">
            <div className="balance-card">
              <div>
                <div className="balance-figure">{account.availableCredits}</div>
                <div className="balance-label">available</div>
              </div>
              <div style={{ textAlign: "right" }}>
                <div className="balance-figure held">{account.reservedCredits}</div>
                <div className="balance-label">held on errands in progress</div>
              </div>
            </div>

            <div className="balance-bar" style={{ marginTop: 14 }} aria-hidden="true">
              <div style={{ display: "flex", height: "100%" }}>
                <div
                  className="balance-bar-available"
                  style={{ width: `${availablePct}%` }}
                />
                <div
                  className="balance-bar-held"
                  style={{ width: `${100 - availablePct}%` }}
                />
              </div>
            </div>

            <p className="balance-label" style={{ marginTop: 10 }}>
              Held credits return to you if an errand is cancelled or expires.
            </p>
          </div>

          <h2 className="section-label">History</h2>

          {/* AI-generated (edited by James Teo): rows labelled by reason, the
              amount with the sign credit-service sends, and no running
              balance, which credit-service does not send. Load more skips
              rows already shown. */}
          {ledger === null && !ledgerFailed && (
            <p className="status" role="status">
              Loading history…
            </p>
          )}

          {ledger !== null && ledger.items.length === 0 && (
            <p className="status">No transactions yet.</p>
          )}

          {ledger !== null && ledger.items.length > 0 && (
            <div className="ledger">
              {ledger.items.map((entry) => (
                <div className="ledger-row" key={entry.id}>
                  <div>
                    <div className="ledger-label">
                      {REASON_LABEL[entry.reason] ?? entry.reason}
                    </div>
                    <div className="ledger-time">{formatWhen(entry.createdAt)}</div>
                  </div>
                  <div>
                    <div
                      className={`ledger-amount ${
                        entry.amount > 0 ? "positive" : ""
                      }`}
                    >
                      {formatAmount(entry.amount)}
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}

          {/* AI-generated (edited by James Teo): Retry repeats the fetch that
              failed: the first page again, or the next page. */}
          {ledgerFailed && (
            <p className="status error" role="alert">
              {ledgerFailureMessage(ledgerFailed, (ledger?.items.length ?? 0) > 0)} Try
              again in a moment.{" "}
              <button
                type="button"
                className="link-btn"
                onClick={() =>
                  ledgerFailed === "more" ? void loadMore() : setLedgerAttempt((n) => n + 1)
                }
              >
                Retry
              </button>
            </p>
          )}

          {hasMore && !ledgerFailed && (
            <div className="btn-row start">
              <button
                type="button"
                className="btn btn-secondary"
                onClick={() => void loadMore()}
                disabled={loadingMore}
              >
                {loadingMore ? "Loading…" : "Load more"}
              </button>
            </div>
          )}
        </>
      )}
    </section>
  );
}

/** "8 Sept, 19:06", as in the mockup. */
function formatWhen(iso: string): string {
  const date = new Date(iso);
  const day = date.toLocaleDateString("en-SG", {
    day: "numeric",
    month: "short",
  });
  const time = date.toLocaleTimeString("en-SG", {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });
  return `${day}, ${time}`;
}
