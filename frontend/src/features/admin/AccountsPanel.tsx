// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-03
// Scope: The dashboard's Accounts tab (D-121 to D-123): search accounts by name
//   or email, list them by status 50 to a page, and open one to review it and
//   act on it (reset its password, suspend or reactivate).
// Author review: PENDING — <reviewer to complete>

import { useEffect, useState, type FormEvent } from "react";
import { Pager } from "../../components/Pager";
import type { ToastMessage } from "../../components/Toast";
import type { OrdersApi } from "../errands/ordersApi";
import { formatDateTime } from "../errands/time";
import type { AccountListItem, AccountStatus, UsersApi } from "../profile/usersApi";
import { AdminView } from "./AdminView";
import { AccountReview } from "./AccountReview";
import { PAGE_SIZE, offsetOfPage, pageCount } from "./auditTypes";

const STATUSES: AccountStatus[] = ["ACTIVE", "SUSPENDED", "UNVERIFIED"];

interface AccountsPanelProps {
  usersApi: UsersApi;
  ordersApi: OrdersApi | null;
  currentUserId: string;
  /** An account to open on arrival, from an errand's detail or an audit row. */
  initialAccountId: string | null;
  onNotify: (message: ToastMessage) => void;
}

/** Search and list on top; the chosen account's actions and review below it. */
export function AccountsPanel({ usersApi, ordersApi, currentUserId, initialAccountId, onNotify }: AccountsPanelProps) {
  const [draft, setDraft] = useState("");
  const [query, setQuery] = useState("");
  const [status, setStatus] = useState<AccountStatus | "">("");
  const [page, setPage] = useState(1);
  const [list, setList] = useState<{ items: AccountListItem[]; total: number; limit: number } | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [reload, setReload] = useState(0);
  const [selected, setSelected] = useState<{ uid: string; lastLoginAt?: string | null } | null>(
    initialAccountId ? { uid: initialAccountId } : null,
  );

  useEffect(() => {
    let live = true;
    setLoading(true);
    usersApi
      .listUsers({
        query,
        status: status || undefined,
        limit: PAGE_SIZE,
        offset: offsetOfPage(page, PAGE_SIZE),
      })
      .then((result) => {
        if (!live) return;
        setList(result);
        setError(null);
      })
      .catch((err) => live && setError(err instanceof Error ? err.message : "Couldn't load accounts."))
      .finally(() => live && setLoading(false));
    return () => {
      live = false;
    };
  }, [usersApi, query, status, page, reload]);

  function search(e: FormEvent) {
    e.preventDefault();
    setQuery(draft.trim());
    setPage(1);
  }

  return (
    <div>
      <form className="controls" onSubmit={search} role="search">
        <input
          className="search-input"
          type="search"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          placeholder="Search by username or email"
          aria-label="Search by username or email"
          autoComplete="off"
        />
        <select
          className="category-select"
          aria-label="Account status"
          value={status}
          onChange={(e) => {
            setStatus(e.target.value as AccountStatus | "");
            setPage(1);
          }}
        >
          <option value="">Any status</option>
          {STATUSES.map((s) => (
            <option key={s} value={s}>
              {s.charAt(0) + s.slice(1).toLowerCase()}
            </option>
          ))}
        </select>
        <button className="btn btn-primary">Search</button>
      </form>

      {error && (
        <p className="status error" role="alert">
          {error}{" "}
          <button type="button" className="link-btn" onClick={() => setReload((n) => n + 1)}>
            Retry
          </button>
        </p>
      )}
      {loading && !list && !error && (
        <p className="status" role="status">
          Loading accounts…
        </p>
      )}
      {list && list.items.length === 0 && !loading && (
        <p className="status" role="status">
          No accounts match.
        </p>
      )}
      {list?.items.map((a) => (
        <button
          key={a.uid}
          type="button"
          className="account-row"
          aria-current={selected?.uid === a.uid}
          onClick={() => setSelected({ uid: a.uid, lastLoginAt: a.lastLoginAt ?? null })}
        >
          <span>
            <strong>{a.username || "—"}</strong>
            <span className="audit-meta"> {a.email}</span>
          </span>
          <span className="audit-meta">
            {a.role} · {a.status}
            {a.lastLoginAt ? ` · last in ${formatDateTime(a.lastLoginAt) || a.lastLoginAt}` : " · never signed in"}
          </span>
        </button>
      ))}
      {list && (
        <Pager page={page} pageCount={pageCount(list.total, list.limit)} total={list.total} busy={loading} onPage={setPage} />
      )}

      {selected && (
        <div className="card" style={{ marginTop: 16 }}>
          <AdminView
            key={selected.uid}
            embedded
            usersApi={usersApi}
            currentUserId={currentUserId}
            initialAccountId={selected.uid}
            onNotify={(m) => {
              onNotify(m);
              // A status change shows in the list too.
              setReload((n) => n + 1);
            }}
          />
          <AccountReview uid={selected.uid} ordersApi={ordersApi} usersApi={usersApi} lastLoginAt={selected.lastLoginAt} />
        </div>
      )}
    </div>
  );
}
