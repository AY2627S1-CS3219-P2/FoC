// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-09
// Scope: The dashboard's Flagged wallets tab (D-150): the credit accounts that
//   reconciliation froze, why, and a Re-verify button on each.
// Author review: PENDING — <reviewer to complete>

import { useCallback, useEffect, useRef, useState } from "react";
import { Pager } from "../../components/Pager";
import type { ToastMessage } from "../../components/Toast";
import { Workspace, WORKSPACE_WIDE } from "../../components/Workspace";
import { useMediaQuery } from "../../lib/useMediaQuery";
import type { CreditAdminApi } from "../credits/creditAdminApi";
import {
  amountsText,
  reasonLabel,
  type FlaggedAccount,
  type ReverifyMessage,
} from "../credits/creditAdminTypes";
import type { OrdersApi } from "../errands/ordersApi";
import { formatDateTime } from "../errands/time";
import type { UsersApi } from "../profile/usersApi";
import { AccountFocusView } from "./AccountFocusView";
import { pageCount, shortId, type Page } from "./auditTypes";
import { ReverifyButton } from "./ReverifyButton";

/** The most rows credit-service sends a page (D-104). */
const PAGE_SIZE = 50;

interface FlaggedWalletsViewProps {
  creditAdminApi: CreditAdminApi;
  /** Names the accounts; without it the rows show the account id. */
  usersApi: UsersApi | null;
  // AI-generated (edited by Claude Code, 2026-11-09): "Review account" opens the account
  // right here (D-154): in a pane beside the list on a wide window, as a page of its own
  // with a way back on a narrow one. So the view needs what an account's page needs.
  ordersApi: OrdersApi | null;
  /** The signed-in admin, so no action is offered on their own account. */
  currentUserId: string;
  onNotify: (message: ToastMessage) => void;
}

/**
 * A flagged wallet is frozen: it cannot reserve, adjust or receive a refund
 * until it is cleared. This lists them, oldest flag first, with the first
 * discrepancy found on each (D-153). Re-verify re-runs the checks and clears the
 * flag only if they all pass; it never edits a balance (D-150).
 */
export function FlaggedWalletsView({ creditAdminApi, usersApi, ordersApi, currentUserId, onNotify }: FlaggedWalletsViewProps) {
  const wide = useMediaQuery(WORKSPACE_WIDE);
  const [selected, setSelected] = useState<{ uid: string; name?: string } | null>(null);
  const [page, setPage] = useState(1);
  const [loaded, setLoaded] = useState<Page<FlaggedAccount> | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [reload, setReload] = useState(0);
  const [message, setMessage] = useState<ReverifyMessage | null>(null);
  const [names, setNames] = useState<Record<string, string>>({});
  const asked = useRef(new Set<string>());

  useEffect(() => {
    let live = true;
    setLoading(true);
    creditAdminApi.listFlaggedAccounts(PAGE_SIZE, (page - 1) * PAGE_SIZE).then(
      (p) => {
        if (!live) return;
        setLoaded(p);
        setError(null);
        setLoading(false);
      },
      (err) => {
        if (!live) return;
        setError(err instanceof Error ? err.message : "Couldn't load the flagged wallets.");
        setLoading(false);
      },
    );
    return () => {
      live = false;
    };
  }, [creditAdminApi, page, reload]);

  // A name is a convenience: a failed lookup leaves the account id showing.
  useEffect(() => {
    if (!usersApi || !loaded) return;
    for (const a of loaded.items) {
      if (asked.current.has(a.userId)) continue;
      asked.current.add(a.userId);
      usersApi.getUser(a.userId).then(
        (u) => u.username && setNames((n) => ({ ...n, [a.userId]: u.username })),
        () => undefined,
      );
    }
  }, [usersApi, loaded]);

  const done = useCallback((m: ReverifyMessage) => {
    setMessage(m);
    setReload((n) => n + 1);
  }, []);

  const pages = loaded ? pageCount(loaded.total, loaded.limit || PAGE_SIZE) : 1;

  const account =
    selected && usersApi ? (
      <AccountFocusView
        usersApi={usersApi}
        ordersApi={ordersApi}
        creditAdminApi={creditAdminApi}
        currentUserId={currentUserId}
        uid={selected.uid}
        name={selected.name}
        backLabel={wide ? undefined : "Flagged wallets"}
        onBack={wide ? undefined : () => setSelected(null)}
        onNotify={onNotify}
      />
    ) : null;

  // A narrow window shows the account as a page of its own; Back returns to the list.
  if (!wide && account) return account;

  const rows = (
    <>
      {error && (
        <p className="status error" role="alert">
          {error}{" "}
          <button type="button" className="link-btn" onClick={() => setReload((n) => n + 1)}>
            Retry
          </button>
        </p>
      )}
      {loading && !loaded && (
        <div role="status" aria-label="Loading flagged wallets">
          <span className="skeleton row" />
          <span className="skeleton row" />
          <span className="skeleton row" />
        </div>
      )}

      {loaded && loaded.items.length === 0 && <p className="status">No wallets are flagged.</p>}
      {loaded && loaded.items.length > 0 && (
        <ul style={{ margin: 0, padding: 0, listStyle: "none" }}>
          {loaded.items.map((a) => {
            const name = names[a.userId];
            const amounts = amountsText(a.expected, a.observed);
            return (
              <li key={a.userId} className={`audit-row ${selected?.uid === a.userId ? "selected" : ""}`}>
                <div className="audit-head">
                  <strong>{name ?? `Account ${shortId(a.userId)}`}</strong>
                  <span className="badge badge-type">Flagged</span>
                </div>
                <p>{reasonLabel(a.reasonCode)}</p>
                {a.detail && <p className="audit-meta">{a.detail}</p>}
                {amounts && <p className="audit-meta">{amounts}</p>}
                <p className="audit-meta">
                  Flagged {formatDateTime(a.flaggedAt) || a.flaggedAt} ·{" "}
                  <span title={a.userId}>account {shortId(a.userId)}</span>
                </p>
                <div className="btn-row" style={{ justifyContent: "flex-start" }}>
                  <ReverifyButton creditAdminApi={creditAdminApi} userId={a.userId} name={name ?? "This"} onDone={done} />
                  <button
                    type="button"
                    className="btn btn-secondary"
                    data-ws-item
                    onClick={() => setSelected({ uid: a.userId, name })}
                  >
                    Review account
                  </button>
                </div>
              </li>
            );
          })}
        </ul>
      )}

      {loaded && loaded.total > 0 && (
        <Pager page={page} pageCount={pages} total={loaded.total} busy={loading} onPage={setPage} />
      )}
    </>
  );

  return (
    <div>
      <p className="page-sub">
        Wallets frozen because their balance stopped matching the ledger. Re-verify checks again and unflags a wallet only if
        every check passes. It never changes a balance.
      </p>

      {message && (
        <p className={message.kind === "error" ? "status error" : "status"} role={message.kind === "error" ? "alert" : "status"}>
          {message.text}
        </p>
      )}

      <Workspace
        narrow="list"
        list={rows}
        detail={wide ? account : null}
        onCloseDetail={() => setSelected(null)}
        emptyText="Pick a wallet to see its account here."
      />
    </div>
  );
}
