// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-03
// Scope: The dashboard's Past errands tab (D-104, D-105): completed, cancelled
//   and expired errands, filtered by state, date range and supplier, sorted by
//   most recent or by supplier name, 50 to a page.
// Author review: PENDING — <reviewer to complete>

import { useEffect, useRef, useState } from "react";
import { Workspace } from "../../components/Workspace";
import { Pager } from "../../components/Pager";
import type { ToastMessage } from "../../components/Toast";
import { ErrandCard } from "../errands/ErrandCard";
import { ERRAND_DETAIL_TITLE_ID, ErrandDetail } from "../errands/ErrandDetail";
import { ErrandFilters, useSupplierChoices, type ErrandFilterValues } from "../errands/ErrandFilters";
import { endedText, orderErrorMessage } from "../errands/format";
import type { OrdersApi } from "../errands/ordersApi";
import type { AdminErrandSort, ErrandList, ErrandStatus } from "../errands/orderTypes";
import type { UsersApi } from "../profile/usersApi";
import type { SuppliersApi } from "../suppliers/suppliersApi";
import { PAGE_SIZE, offsetOfPage, pageCount } from "./auditTypes";

/** The final states a past errand can be in. */
const FINAL: ErrandStatus[] = ["COMPLETED", "CANCELLED", "EXPIRED"];

const SORTS: { value: AdminErrandSort; label: string }[] = [
  { value: "recent", label: "Most recent" },
  { value: "supplier_name", label: "Supplier name (A–Z)" },
];

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
  const [filters, setFilters] = useState<ErrandFilterValues>({
    state: "",
    supplierId: "",
    from: "",
    to: "",
    sort: "recent",
  });
  const { state, supplierId, from, to, sort } = filters;
  const [page, setPage] = useState(1);
  const [list, setList] = useState<ErrandList | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [reload, setReload] = useState(0);
  const suppliers = useSupplierChoices(suppliersApi);
  const [open, setOpen] = useState<string | null>(null);
  const headingRef = useRef<HTMLHeadingElement>(null);

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

  return (
    <div>
      <h2 className="section-label" ref={headingRef} tabIndex={-1}>
        Past errands
      </h2>
      <ErrandFilters
        idPrefix="past"
        value={filters}
        states={FINAL}
        dateLabel="Ended"
        sorts={SORTS}
        suppliers={suppliers}
        onChange={(next) => {
          setFilters(next);
          setPage(1);
        }}
      />

      {/* AI-generated (edited by Claude Code, 2026-11-09): the list and the errand's
          detail side by side on a wide window, the detail in a dialog on a narrow
          one (D-154). */}
      <Workspace
        list={
          <>
      {error && (
          <p className="status error" role="alert">
            Couldn&rsquo;t load past errands. {error}{" "}
            <button type="button" className="link-btn" onClick={() => setReload((n) => n + 1)}>
              Retry
            </button>
          </p>
        )}
        {loading && !list && !error && (
          <div role="status" aria-label="Loading errands">
            <span className="skeleton row" />
            <span className="skeleton row" />
            <span className="skeleton row" />
          </div>
        )}
        {list && list.items.length === 0 && !loading && (
          <p className="status" role="status">
            No past errands match.
          </p>
        )}
        {list?.items.map((errand) => (
          <ErrandCard
            key={errand.id}
            errand={errand}
            line={endedText(errand)}
            selected={open === errand.id}
            onOpen={() => setOpen(errand.id)}
          />
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
          </>
        }
        detail={
          open ? (
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
          ) : null
        }
        onCloseDetail={() => setOpen(null)}
        emptyText="Pick an errand to see it here."
        detailTitleId={ERRAND_DETAIL_TITLE_ID}
        fallbackFocus={headingRef}
      />
    </div>
  );
}
