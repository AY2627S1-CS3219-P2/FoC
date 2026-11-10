// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-03
// Scope: The dashboard's Audit log tab (D-104, D-107, D-110): the three
//   services' admin audit logs read through their own endpoints and shown as
//   one list, newest first, filterable and paged by 50.
// Author review: PENDING — <reviewer to complete>

import { useEffect, useState } from "react";
import { Pager } from "../../components/Pager";
import type { CreditAdminApi } from "../credits/creditAdminApi";
import type { OrdersApi } from "../errands/ordersApi";
import type { UsersApi } from "../profile/usersApi";
import type { SuppliersApi } from "../suppliers/suppliersApi";
import { AuditList } from "./AuditList";
import {
  AUDIT_ACTIONS,
  AUDIT_ACTION_LABEL,
  AUDIT_OUTCOMES,
  AUDIT_SOURCES,
  AUDIT_SOURCE_LABEL,
  PAGE_SIZE,
  combinePages,
  offsetOfPage,
  type AuditEntry,
  type AuditFilter,
  type AuditOutcome,
  type AuditSource,
  type Page,
} from "./auditTypes";

interface AuditLogViewProps {
  usersApi: UsersApi | null;
  ordersApi: OrdersApi | null;
  suppliersApi: SuppliersApi;
  // AI-generated (edited by Claude Code, 2026-11-09): credit-service's log is the fourth (D-152).
  /** Null in a fixture build, which has no credit-service to call. */
  creditAdminApi: CreditAdminApi | null;
  onOpenAccount: (uid: string) => void;
}

interface Loaded {
  items: AuditEntry[];
  total: number;
  pageCount: number;
  /** Logs that could not be read, so a partial list is not mistaken for the whole. */
  unavailable: AuditSource[];
}

/**
 * Each service keeps and serves its own log (D-110), so this asks the logs the
 * filter reaches and merges their pages. With every source chosen the view is
 * "page N of each": page 2 holds rows 51–100 of each log, merged by time.
 */
export function AuditLogView({ usersApi, ordersApi, suppliersApi, creditAdminApi, onOpenAccount }: AuditLogViewProps) {
  const [source, setSource] = useState<AuditSource | "">("");
  const [action, setAction] = useState("");
  const [outcome, setOutcome] = useState<AuditOutcome | "">("");
  const [targetId, setTargetId] = useState("");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [page, setPage] = useState(1);
  const [loaded, setLoaded] = useState<Loaded | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [reload, setReload] = useState(0);

  const sources = AUDIT_SOURCES.filter((s) => !source || s === source).filter(
    // An action belongs to one service, so naming one leaves the others out.
    (s) => !action || AUDIT_ACTIONS[s].includes(action),
  );
  const actionChoices = source ? AUDIT_ACTIONS[source] : AUDIT_SOURCES.flatMap((s) => AUDIT_ACTIONS[s]);

  useEffect(() => {
    let live = true;
    const filter: AuditFilter = {
      action: action || undefined,
      outcome: outcome || undefined,
      targetId: targetId.trim() || undefined,
      from: from || undefined,
      to: to || undefined,
      limit: PAGE_SIZE,
      offset: offsetOfPage(page, PAGE_SIZE),
    };
    const clients: Record<AuditSource, (() => Promise<Page<AuditEntry>>) | null> = {
      accounts: usersApi ? () => usersApi.listAuditLog(filter) : null,
      errands: ordersApi ? () => ordersApi.listAuditLog(filter) : null,
      suppliers: () => suppliersApi.listAuditLog(filter),
      credits: creditAdminApi ? () => creditAdminApi.listAuditLog(filter) : null,
    };
    setLoading(true);
    void (async () => {
      const wanted = sources.filter((s) => clients[s]);
      const results = await Promise.allSettled(wanted.map((s) => clients[s]!()));
      if (!live) return;
      const pages: Page<AuditEntry>[] = [];
      const unavailable: AuditSource[] = sources.filter((s) => !clients[s]);
      results.forEach((r, i) => {
        if (r.status === "fulfilled") pages.push(r.value);
        else unavailable.push(wanted[i]!);
      });
      if (pages.length === 0 && unavailable.length > 0) {
        setError("Couldn't load the audit log.");
        setLoaded(null);
      } else {
        setError(null);
        setLoaded({ ...combinePages(pages), unavailable });
      }
      setLoading(false);
    })();
    return () => {
      live = false;
    };
    // `sources` is derived from source and action.
  }, [source, action, outcome, targetId, from, to, page, reload, usersApi, ordersApi, suppliersApi, creditAdminApi]);

  /** A new filter starts again from the first page. */
  function filtered<T>(set: (v: T) => void) {
    return (v: T) => {
      set(v);
      setPage(1);
    };
  }

  return (
    <div>
      <p className="page-sub">
        What admins did, and what was refused. Rows are kept for 6 months and cannot be changed.
      </p>
      <div className="filter-row">
        <div className="field">
          <label htmlFor="audit-source">Service</label>
          <select
            id="audit-source"
            value={source}
            onChange={(e) => {
              filtered(setSource)(e.target.value as AuditSource | "");
              setAction("");
            }}
          >
            <option value="">All</option>
            {AUDIT_SOURCES.map((s) => (
              <option key={s} value={s}>
                {AUDIT_SOURCE_LABEL[s]}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor="audit-action">Action</label>
          <select id="audit-action" value={action} onChange={(e) => filtered(setAction)(e.target.value)}>
            <option value="">Any</option>
            {actionChoices.map((a) => (
              <option key={a} value={a}>
                {AUDIT_ACTION_LABEL[a] ?? a}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor="audit-outcome">Outcome</label>
          <select
            id="audit-outcome"
            value={outcome}
            onChange={(e) => filtered(setOutcome)(e.target.value as AuditOutcome | "")}
          >
            <option value="">Any</option>
            {AUDIT_OUTCOMES.map((o) => (
              <option key={o} value={o}>
                {o}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor="audit-from">From</label>
          <input id="audit-from" type="date" value={from} onChange={(e) => filtered(setFrom)(e.target.value)} />
        </div>
        <div className="field">
          <label htmlFor="audit-to">Before</label>
          <input id="audit-to" type="date" value={to} onChange={(e) => filtered(setTo)(e.target.value)} />
        </div>
        <div className="field">
          <label htmlFor="audit-target">Target ID</label>
          <input
            id="audit-target"
            type="text"
            value={targetId}
            onChange={(e) => filtered(setTargetId)(e.target.value)}
            placeholder="Account, errand or supplier ID"
            spellCheck={false}
            autoComplete="off"
          />
        </div>
      </div>

      {error && (
        <p className="status error" role="alert">
          {error}{" "}
          <button type="button" className="link-btn" onClick={() => setReload((n) => n + 1)}>
            Retry
          </button>
        </p>
      )}
      {loading && !loaded && !error && (
        <p className="status" role="status">
          Loading the audit log…
        </p>
      )}
      {loaded && loaded.unavailable.length > 0 && (
        <p className="status error" role="alert">
          Not shown: {loaded.unavailable.map((s) => AUDIT_SOURCE_LABEL[s]).join(", ")}. Those logs could not be
          read.
        </p>
      )}
      {loaded && loaded.items.length === 0 && !loading && (
        <p className="status" role="status">
          No audit rows match.
        </p>
      )}
      {loaded && loaded.items.length > 0 && (
        <div className="card">
          <AuditList entries={loaded.items} onOpenAccount={onOpenAccount} />
        </div>
      )}
      {loaded && (
        <Pager page={page} pageCount={loaded.pageCount} total={loaded.total} busy={loading} onPage={setPage} />
      )}
    </div>
  );
}
