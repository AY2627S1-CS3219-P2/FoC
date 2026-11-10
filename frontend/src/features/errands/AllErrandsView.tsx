// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-18
// Scope: All errands, the admin's view of every errand that is not final,
//   oldest state change first and polled, with each errand's detail (cancel,
//   complete, the parties and Look up account) in a modal.
// Author review: James Teo — reviewed and verified.

import { useRef, useState } from "react";
import { Workspace } from "../../components/Workspace";
import { Pager } from "../../components/Pager";
import type { ToastMessage } from "../../components/Toast";
import { usePolling } from "../../lib/polling";
import { PAGE_SIZE, offsetOfPage, pageCount } from "../admin/auditTypes";
import type { UsersApi } from "../profile/usersApi";
import type { SuppliersApi } from "../suppliers/suppliersApi";
import { ErrandCard } from "./ErrandCard";
import { ErrandFilters, useSupplierChoices, type ErrandFilterValues } from "./ErrandFilters";
import { ERRAND_DETAIL_TITLE_ID, ErrandDetail, type ErrandSnapshot } from "./ErrandDetail";
import { orderErrorMessage, timeInStateText } from "./format";
import type { OrdersApi } from "./ordersApi";
import type { AdminErrandSort, Errand, ErrandStatus } from "./orderTypes";
import { clockOffset, serverNow, useNow } from "./time";

interface AllErrandsViewProps {
  ordersApi: OrdersApi;
  suppliersApi: SuppliersApi;
  usersApi: UsersApi | null;
  /** The signed-in admin. */
  userId: string;
  onNotify: (message: ToastMessage) => void;
  /** Opens the Admin screen with that account looked up. */
  onLookUpAccount: (uid: string) => void;
  // AI-generated (edited by Claude Code, 2026-11-03): the dashboard's Active
  // errands tab holds this list under its own heading.
  /** Shown as a tab of the dashboard: a section heading and no page title. */
  embedded?: boolean;
}

interface ActiveList {
  items: Errand[];
  /** Server clock minus device clock, for the time-in-state lines. */
  offset: number;
  total: number;
  limit: number;
}

/** The states an errand that is not finished can be in. */
const ACTIVE_STATES: ErrandStatus[] = ["OPEN", "ACCEPTED", "PICKED_UP", "DELIVERED"];

const SORTS: { value: AdminErrandSort; label: string }[] = [
  { value: "oldest", label: "Longest in its state" },
  { value: "recent", label: "Most recent" },
  { value: "supplier_name", label: "Supplier name (A–Z)" },
];

/**
 * App shows this to ADMIN sessions only, which is an affordance: order-service
 * answers the list, and allows cancel and complete, for ADMIN tokens alone.
 * Stalled errands come first, because the list is ordered by how long each has
 * been in its state.
 */
export function AllErrandsView({
  ordersApi,
  suppliersApi,
  usersApi,
  userId,
  onNotify,
  onLookUpAccount,
  embedded = false,
}: AllErrandsViewProps) {
  const [filters, setFilters] = useState<ErrandFilterValues>({
    state: "",
    supplierId: "",
    from: "",
    to: "",
    sort: "oldest",
  });
  const [page, setPage] = useState(1);
  const suppliers = useSupplierChoices(suppliersApi);
  const [list, setList] = useState<ActiveList | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [open, setOpen] = useState<{ id: string; initial?: ErrandSnapshot } | null>(null);
  const now = useNow(1000);
  // Takes focus when a closed detail's card has left the list.
  const headingRef = useRef<HTMLHeadingElement>(null);

  const refresh = usePolling(
    async (isActive) => {
      try {
        const result = await ordersApi.listErrandsAsAdmin({
          scope: "active",
          state: filters.state || undefined,
          supplierId: filters.supplierId || undefined,
          from: filters.from || undefined,
          to: filters.to || undefined,
          sort: filters.sort,
          limit: PAGE_SIZE,
          offset: offsetOfPage(page, PAGE_SIZE),
        });
        if (!isActive()) return;
        setList({
          items: result.items,
          offset: clockOffset(result.serverTime),
          total: result.total ?? result.items.length,
          limit: result.limit ?? PAGE_SIZE,
        });
        setError(null);
      } catch (err) {
        if (isActive()) setError(orderErrorMessage(err));
        throw err;
      }
    },
    5_000,
    [ordersApi, filters, page],
  );
  const filtering = filters.state !== "" || filters.supplierId !== "" || filters.from !== "" || filters.to !== "";

  const at = list ? serverNow(list.offset, now) : now;

  const Heading = embedded ? "h2" : "h1";
  return (
    <section>
      <div className="page-head">
        <Heading ref={headingRef} tabIndex={-1} className={embedded ? "section-label" : undefined}>
          {embedded ? "Active errands" : "All errands"}
        </Heading>
        <p className="page-sub">
          Every errand that isn&rsquo;t finished yet, the longest in its state first unless you
          sort it differently. Open one to cancel it or mark it completed when it has stalled.
        </p>
      </div>

      <ErrandFilters
        idPrefix="active"
        value={filters}
        states={ACTIVE_STATES}
        dateLabel="In state"
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
            {list ? "Couldn't refresh the list. " : "Couldn't load errands. "}
            {error}{" "}
            <button type="button" className="link-btn" onClick={refresh}>
              Retry
            </button>
          </p>
        )}
  
        {!list && !error && (
          <p className="status" role="status">
            Loading errands…
          </p>
        )}
  
        {list && list.items.length === 0 && (
          <p className="status" role="status">
            {filtering ? "No errands match." : "No errands in progress."}
          </p>
        )}
  
        {list?.items.map((errand) => (
          <ErrandCard
            key={errand.id}
            errand={errand}
            line={timeInStateText(errand, at)}
            onOpen={() =>
              setOpen({
                id: errand.id,
                initial: { errand, serverTime: new Date(at).toISOString() },
              })
            }
          />
        ))}
  
        {list && (
          <Pager
            page={page}
            pageCount={pageCount(list.total, list.limit)}
            total={list.total}
            busy={false}
            onPage={setPage}
          />
        )}
          </>
        }
        detail={
          open ? (
            <ErrandDetail
                errandId={open.id}
                initial={open.initial}
                ordersApi={ordersApi}
                suppliersApi={suppliersApi}
                usersApi={usersApi}
                viewerId={userId}
                viewerRole="ADMIN"
                available={null}
                onChanged={refresh}
                onGone={(message) => {
                  setOpen(null);
                  onNotify({ text: message, kind: "error" });
                  refresh();
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
    </section>
  );
}
