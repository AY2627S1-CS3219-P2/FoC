// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-18
// Scope: All errands, the admin's view of every errand that is not final,
//   oldest state change first and polled, with each errand's detail (cancel,
//   complete, the parties and Look up account) in a modal.
// Author review: James Teo — reviewed and verified.

import { useRef, useState } from "react";
import { Modal } from "../../components/Modal";
import type { ToastMessage } from "../../components/Toast";
import { usePolling } from "../../lib/polling";
import type { UsersApi } from "../profile/usersApi";
import type { SuppliersApi } from "../suppliers/suppliersApi";
import { ErrandCard } from "./ErrandCard";
import { ERRAND_DETAIL_TITLE_ID, ErrandDetail, type ErrandSnapshot } from "./ErrandDetail";
import { orderErrorMessage, timeInStateText } from "./format";
import type { OrdersApi } from "./ordersApi";
import type { Errand } from "./orderTypes";
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
}

interface ActiveList {
  items: Errand[];
  offset: number;
}

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
}: AllErrandsViewProps) {
  const [list, setList] = useState<ActiveList | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [open, setOpen] = useState<{ id: string; initial?: ErrandSnapshot } | null>(null);
  const now = useNow(1000);
  // Takes focus when a closed detail's card has left the list.
  const headingRef = useRef<HTMLHeadingElement>(null);

  const refresh = usePolling(
    async (isActive) => {
      try {
        const result = await ordersApi.listActiveForAdmin();
        if (!isActive()) return;
        setList({ items: result.items, offset: clockOffset(result.serverTime) });
        setError(null);
      } catch (err) {
        if (isActive()) setError(orderErrorMessage(err));
        throw err;
      }
    },
    5_000,
    [ordersApi],
  );

  const at = list ? serverNow(list.offset, now) : now;

  return (
    <section>
      <div className="page-head">
        <h1 ref={headingRef} tabIndex={-1}>
          All errands
        </h1>
        <p className="page-sub">
          Every errand that isn&rsquo;t finished yet, the longest in its state first. Open one
          to cancel it or mark it completed when it has stalled.
        </p>
      </div>

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
          No errands in progress.
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

      {open && (
        <Modal
          onClose={() => setOpen(null)}
          labelledBy={ERRAND_DETAIL_TITLE_ID}
          fallbackFocus={headingRef}
        >
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
        </Modal>
      )}
    </section>
  );
}
