// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-03
// Scope: The account review (D-117): what an admin sees about one account in a
//   single place, put together from four calls to two services: the errands the
//   user is in, their counts by state, and the audit rows about the account.
// Author review: PENDING — <reviewer to complete>

import { useEffect, useState } from "react";
import { orderErrorMessage, STATE_LABEL } from "../errands/format";
import type { OrdersApi } from "../errands/ordersApi";
import { stateBadgeClass } from "../errands/ErrandCard";
import { endedText } from "../errands/format";
import type { Errand, ErrandCounts } from "../errands/orderTypes";
import { formatDateTime } from "../errands/time";
import type { UsersApi } from "../profile/usersApi";
import { AuditList } from "./AuditList";
import { countsSummary, type AuditEntry } from "./auditTypes";

/** How many past errands and audit rows the review shows; the tabs hold the rest. */
const REVIEW_ROWS = 10;

interface Section<T> {
  data: T | null;
  error: string | null;
}

interface AccountReviewProps {
  uid: string;
  ordersApi: OrdersApi | null;
  usersApi: UsersApi;
  /** A time; null when the account has never signed in; absent when that is not known. */
  lastLoginAt?: string | null;
}

/**
 * Each part loads on its own, so one service being down leaves the rest of the
 * review readable. The errands and counts come from order-service, which holds
 * them; the audit rows about the account from user-service, whose log they are.
 */
export function AccountReview({ uid, ordersApi, usersApi, lastLoginAt }: AccountReviewProps) {
  const [counts, setCounts] = useState<Section<ErrandCounts>>({ data: null, error: null });
  const [active, setActive] = useState<Section<Errand[]>>({ data: null, error: null });
  const [past, setPast] = useState<Section<Errand[]>>({ data: null, error: null });
  const [audit, setAudit] = useState<Section<AuditEntry[]>>({ data: null, error: null });

  useEffect(() => {
    let live = true;
    setCounts({ data: null, error: null });
    setActive({ data: null, error: null });
    setPast({ data: null, error: null });
    setAudit({ data: null, error: null });

    /** Runs one call and files its result, or its message, into `set`. */
    function load<T>(call: () => Promise<T>, set: (s: Section<T>) => void, message: (err: unknown) => string) {
      call().then(
        (data) => live && set({ data, error: null }),
        (err) => live && set({ data: null, error: message(err) }),
      );
    }
    const fail = (err: unknown) => (err instanceof Error ? err.message : "Couldn't load this.");

    if (ordersApi) {
      load(() => ordersApi.getUserErrandCounts(uid), setCounts, orderErrorMessage);
      load(async () => (await ordersApi.listErrandsAsAdmin({ scope: "active", userId: uid, limit: 50 })).items, setActive, orderErrorMessage);
      load(
        async () => (await ordersApi.listErrandsAsAdmin({ scope: "history", userId: uid, limit: REVIEW_ROWS })).items,
        setPast,
        orderErrorMessage,
      );
    }
    load(async () => (await usersApi.listAuditLog({ targetId: uid, limit: REVIEW_ROWS })).items, setAudit, fail);
    return () => {
      live = false;
    };
  }, [uid, ordersApi, usersApi]);

  return (
    <div className="review-section">
      <h3 className="section-label">Account review</h3>
      <p className="audit-meta">Last signed in: {lastLoginAt === null ? "never" : lastLoginAt ? formatDateTime(lastLoginAt) || lastLoginAt : "not known"}</p>

      {ordersApi ? (
        <>
          <ReviewBlock title="Errand counts" section={counts}>
            {(c) => (
              <>
                <CountLine label="As requester" counts={c.requested} />
                <CountLine label="As courier" counts={c.assigned} />
              </>
            )}
          </ReviewBlock>
          <ReviewBlock title="In progress" section={active}>
            {(items) => (items.length ? <ErrandLines errands={items} line={() => null} /> : <p className="audit-meta">None.</p>)}
          </ReviewBlock>
          <ReviewBlock title="Most recent past errands" section={past}>
            {(items) => (items.length ? <ErrandLines errands={items} line={endedText} /> : <p className="audit-meta">None.</p>)}
          </ReviewBlock>
        </>
      ) : (
        <p className="audit-meta">The errand history needs order-service.</p>
      )}

      <ReviewBlock title="Admin actions on this account" section={audit}>
        {(items) => (items.length ? <AuditList entries={items} /> : <p className="audit-meta">None recorded.</p>)}
      </ReviewBlock>
    </div>
  );
}

interface ReviewBlockProps<T> {
  title: string;
  section: Section<T>;
  children: (data: T) => React.ReactNode;
}

function ReviewBlock<T>({ title, section, children }: ReviewBlockProps<T>) {
  return (
    <div className="review-section">
      <h4 className="field-label">{title}</h4>
      {section.error && (
        <p className="status error" role="alert">
          {section.error}
        </p>
      )}
      {!section.error && !section.data && (
        <p className="status" role="status">
          Loading…
        </p>
      )}
      {section.data && children(section.data)}
    </div>
  );
}

function CountLine({ label, counts }: { label: string; counts: Partial<Record<string, number>> }) {
  const entries = countsSummary(counts);
  return (
    <div>
      <span className="audit-meta">{label}: </span>
      {entries.length === 0 ? (
        <span className="audit-meta">none</span>
      ) : (
        <ul className="review-counts">
          {entries.map(([state, n]) => (
            <li key={state} className="badge badge-type">
              {STATE_LABEL[state as keyof typeof STATE_LABEL] ?? state} {n}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function ErrandLines({ errands, line }: { errands: Errand[]; line: (e: Errand) => string | null }) {
  return (
    <ul style={{ margin: 0, padding: 0, listStyle: "none" }}>
      {errands.map((e) => (
        <li key={e.id} className="audit-row">
          <div className="audit-head">
            <strong>{e.itemDescription}</strong>
            <span className={`badge ${stateBadgeClass(e.state)}`}>{STATE_LABEL[e.state] ?? e.state}</span>
          </div>
          <p className="audit-meta">
            {e.supplier.name} → {e.deliveryLocation}
            {line(e) ? ` · ${line(e)}` : ""}
          </p>
        </li>
      ))}
    </ul>
  );
}
