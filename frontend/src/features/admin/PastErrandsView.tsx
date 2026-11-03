// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-03
// Scope: The dashboard's Past errands tab (D-104, D-105): completed, cancelled
//   and expired errands, filtered by state, date range and supplier, sorted by
//   most recent or by supplier name, 50 to a page.
// Author review: PENDING — <reviewer to complete>

import { useEffect, useRef, useState } from "react";
import { Modal } from "../../components/Modal";
import { Pager } from "../../components/Pager";
import type { ToastMessage } from "../../components/Toast";
import { ErrandCard } from "../errands/ErrandCard";
import { ERRAND_DETAIL_TITLE_ID, ErrandDetail } from "../errands/ErrandDetail";
import { endedText, orderErrorMessage, STATE_LABEL } from "../errands/format";
import type { OrdersApi } from "../errands/ordersApi";
import type { AdminErrandSort, ErrandList, ErrandStatus } from "../errands/orderTypes";
import type { UsersApi } from "../profile/usersApi";
import type { Supplier } from "../suppliers/types";
import type { SuppliersApi } from "../suppliers/suppliersApi";
import { PAGE_SIZE, offsetOfPage, pageCount } from "./auditTypes";

/** The final states a past errand can be in. */
const FINAL: ErrandStatus[] = ["COMPLETED", "CANCELLED", "EXPIRED"];

interface PastErrandsViewProps {
  ordersApi: OrdersApi;
  suppliersApi: SuppliersApi;
  usersApi: UsersApi | null;
  userId: string;
  onNotify: (message: ToastMessage) => void;
  onLookUpAccount: (uid: string) => void;
}

/** Reads past errands from order-service's admin list; it owns the filtering, sorting and paging. */
export function PastErrandsView({
  ordersApi,
  suppliersApi,
  usersApi,
  userId,
  onNotify,
  onLookUpAccount,
}: PastErrandsViewProps) {
  const [state, setState] = useState<ErrandStatus | "">("");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [supplierId, setSupplierId] = useState("");
  const [sort, setSort] = useState<AdminErrandSort>("recent");
  const [page, setPage] = useState(1);
  const [list, setList] = useState<ErrandList | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [reload, setReload] = useState(0);
  const [suppliers, setSuppliers] = useState<Supplier[]>([]);
  const [open, setOpen] = useState<string | null>(null);
  const headingRef = useRef<HTMLHeadingElement>(null);

  // The supplier filter is a list of names; a failed load only leaves it empty.
  useEffect(() => {
    let live = true;
    suppliersApi
      .listSuppliers()
      .then((s) => live && setSuppliers(s))
      .catch(() => undefined);
    return () => {
      live = false;
    };
  }, [suppliersApi]);

  useEffect(() => {
    let live = true;
    setLoading(true);
    ordersApi
      .listErrandsAsAdmin({
        scope: "history",
        state: state || undefined,
        from: from || undefined,
        to: to || undefined,
        supplierId: supplierId || undefined,
        sort,
        limit: PAGE_SIZE,
        offset: offsetOfPage(page, PAGE_SIZE),
      })
      .then((result) => {
        if (!live) return;
        setList(result);
        setError(null);
      })
      .catch((err) => live && setError(orderErrorMessage(err)))
      .finally(() => live && setLoading(false));
    return () => {
      live = false;
    };
  }, [ordersApi, state, from, to, supplierId, sort, page, reload]);

  function filtered<T>(set: (v: T) => void) {
    return (v: T) => {
      set(v);
      setPage(1);
    };
  }

  return (
    <div>
      <h2 className="section-label" ref={headingRef} tabIndex={-1}>
        Past errands
      </h2>
      <div className="filter-row">
        <div className="field">
          <label htmlFor="past-state">State</label>
          <select id="past-state" value={state} onChange={(e) => filtered(setState)(e.target.value as ErrandStatus | "")}>
            <option value="">Any</option>
            {FINAL.map((s) => (
              <option key={s} value={s}>
                {STATE_LABEL[s]}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor="past-supplier">Supplier</label>
          <select id="past-supplier" value={supplierId} onChange={(e) => filtered(setSupplierId)(e.target.value)}>
            <option value="">Any</option>
            {suppliers.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor="past-from">Ended from</label>
          <input id="past-from" type="date" value={from} onChange={(e) => filtered(setFrom)(e.target.value)} />
        </div>
        <div className="field">
          <label htmlFor="past-to">Ended before</label>
          <input id="past-to" type="date" value={to} onChange={(e) => filtered(setTo)(e.target.value)} />
        </div>
        <div className="field">
          <label htmlFor="past-sort">Sort</label>
          <select id="past-sort" value={sort} onChange={(e) => filtered(setSort)(e.target.value as AdminErrandSort)}>
            <option value="recent">Most recent</option>
            <option value="supplier_name">Supplier name (A–Z)</option>
          </select>
        </div>
      </div>

      {error && (
        <p className="status error" role="alert">
          Couldn&rsquo;t load past errands. {error}{" "}
          <button type="button" className="link-btn" onClick={() => setReload((n) => n + 1)}>
            Retry
          </button>
        </p>
      )}
      {loading && !list && !error && (
        <p className="status" role="status">
          Loading errands…
        </p>
      )}
      {list && list.items.length === 0 && !loading && (
        <p className="status" role="status">
          No past errands match.
        </p>
      )}
      {list?.items.map((errand) => (
        <ErrandCard key={errand.id} errand={errand} line={endedText(errand)} onOpen={() => setOpen(errand.id)} />
      ))}
      {list && list.total !== undefined && (
        <Pager
          page={page}
          pageCount={pageCount(list.total, list.limit ?? PAGE_SIZE)}
          total={list.total}
          busy={loading}
          onPage={setPage}
        />
      )}

      {open && (
        <Modal onClose={() => setOpen(null)} labelledBy={ERRAND_DETAIL_TITLE_ID} fallbackFocus={headingRef}>
          <ErrandDetail
            errandId={open}
            ordersApi={ordersApi}
            suppliersApi={suppliersApi}
            usersApi={usersApi}
            viewerId={userId}
            viewerRole="ADMIN"
            available={null}
            onChanged={() => setReload((n) => n + 1)}
            onGone={(message) => {
              setOpen(null);
              onNotify({ text: message, kind: "error" });
              setReload((n) => n + 1);
            }}
            onLookUpAccount={onLookUpAccount}
          />
        </Modal>
      )}
    </div>
  );
}
