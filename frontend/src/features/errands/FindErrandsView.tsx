// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-18
// Scope: Find errands, the courier's view of order-service's open list:
//   polled, the caller's own errands and any that do not offer them accept
//   left out, Accept on each card with a refusal shown on that card, and the
//   errand's detail in a modal.
// Author review: James Teo — reviewed and verified.

import { useEffect, useRef, useState } from "react";
import { Modal } from "../../components/Modal";
import type { ToastMessage } from "../../components/Toast";
import { usePolling } from "../../lib/polling";
import type { UsersApi } from "../profile/usersApi";
import type { SuppliersApi } from "../suppliers/suppliersApi";
import { ErrandCard } from "./ErrandCard";
import { ERRAND_DETAIL_TITLE_ID, ErrandDetail, buttonsFor, type ErrandSnapshot } from "./ErrandDetail";
import {
  ACTION_BUSY_LABEL,
  ACTION_LABEL,
  actionDoneText,
  MSG_NOT_OPEN_ANY_MORE,
  orderErrorMessage,
  progressText,
} from "./format";
import {
  destinationGroups,
  filterErrands,
  isFiltered,
  NO_FILTERS,
  pickupOptions,
  TIME_LEFT_OPTIONS,
  type FindFilters,
  type TimeLeft,
} from "./findFilters";
import type { OrdersApi } from "./ordersApi";
import type { CampusLocation, Errand } from "./orderTypes";
import { formatDistance, rankByDistance, type LatLon, type SortOrder } from "./nearby";
import { useShownTracking, type ShownErrand } from "./shownTracking";
import { clockOffset, serverNow, useNow } from "./time";
import { useCourierLocation } from "./useCourierLocation";

// AI-generated (edited by PENDING): what Find errands reports for errand
// ranking (D-092). App passes these once order-service has somewhere to send
// them; until then nothing is recorded.
/** An errand whose whole card came into view, with the filters set at the time. */
export interface ShownRecord extends ShownErrand {
  filters: FindFilters;
}

interface FindErrandsViewProps {
  ordersApi: OrdersApi;
  suppliersApi: SuppliersApi;
  usersApi: UsersApi | null;
  /** The signed-in student, whose own errands are left out. */
  userId: string;
  available: number | null;
  onNotify: (message: ToastMessage) => void;
  /** After an action in the detail, so App refreshes the balance. */
  onCreditsChanged: () => void;
  // AI-generated (edited by PENDING): D-092's two records.
  /** Called once per visit for each errand whose whole card comes into view. */
  onShown?: (shown: ShownRecord) => void;
  /** Called each time the courier changes a filter, with all the filters as set. */
  onSearch?: (filters: FindFilters) => void;
}

/** The open list as last fetched, with the server clock's offset at that moment. */
interface OpenList {
  items: Errand[];
  offset: number;
}

/** The errand whose detail is open, and what the action that opened it did. */
interface OpenDetail {
  id: string;
  initial?: ErrandSnapshot;
  notice?: string;
}

/** Why the last Accept was refused, kept until the next Accept. */
interface Refusal {
  errandId: string;
  /** For the message above the list once the card has gone. */
  item: string;
  message: string;
}

/**
 * Errands this student can accept, newest first. order-service lists the
 * caller's own errands here too, since the route ignores who asks; they are
 * left out, because accepting your own errand is refused. So is any other
 * errand whose `actions` do not offer accept to this caller, such as one they
 * withdrew from: the list is for errands they can take. Polled every 5 s
 * while on screen (F7.5).
 */
export function FindErrandsView({
  ordersApi,
  suppliersApi,
  usersApi,
  userId,
  available,
  onNotify,
  onCreditsChanged,
  onShown,
  onSearch,
}: FindErrandsViewProps) {
  const [list, setList] = useState<OpenList | null>(null);
  // AI-generated (edited by PENDING): the filters (D-092) and the locations their
  // destination choice is grouped by.
  const [filters, setFilters] = useState<FindFilters>(NO_FILTERS);
  const [locations, setLocations] = useState<CampusLocation[]>([]);
  const filtersRef = useRef(filters);
  filtersRef.current = filters;
  const track = useShownTracking(
    onShown && ((shown) => onShown({ ...shown, filters: filtersRef.current })),
  );
  // AI-generated (edited by PENDING): D-098's default ranking, nearest pickup
  // first, worked out here so the courier's location never leaves the page.
  const { location: courier, request: requestLocation } = useCourierLocation();
  const [sortOrder, setSortOrder] = useState<SortOrder>("nearest");
  const [pickups, setPickups] = useState<ReadonlyMap<string, LatLon>>(new Map());
  const [error, setError] = useState<string | null>(null);
  const [accepting, setAccepting] = useState<string | null>(null);
  const [refusal, setRefusal] = useState<Refusal | null>(null);
  const [open, setOpen] = useState<OpenDetail | null>(null);
  const now = useNow(1000);

  const headingRef = useRef<HTMLHeadingElement>(null);
  const refusalRef = useRef<HTMLParagraphElement>(null);
  const acceptButtons = useRef(new Map<string, HTMLButtonElement>());
  // Set when an Accept is refused, cleared once focus is back on that card.
  const refocusAccept = useRef(false);

  const refresh = usePolling(
    async (isActive) => {
      try {
        const result = await ordersApi.listOpen();
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

  async function accept(errand: Errand) {
    setAccepting(errand.id);
    setRefusal(null);
    try {
      const detail = await ordersApi.accept(errand.id, errand.version);
      // The detail opens on the accepted errand and says so inside it.
      setOpen({
        id: detail.errand.id,
        initial: detail,
        notice: actionDoneText("accept", detail.errand),
      });
    } catch (err) {
      // Someone else accepted first, the courier is at their limit, or they
      // withdrew from it before: order-service says which, on the card.
      refocusAccept.current = true;
      setRefusal({
        errandId: errand.id,
        item: errand.itemDescription,
        message: orderErrorMessage(err),
      });
    } finally {
      setAccepting(null);
      refresh();
    }
  }

  // AI-generated (edited by PENDING): the destination filter groups places by
  // area. If the list cannot be fetched, that filter offers no choices and
  // the rest of the screen works as before.
  useEffect(() => {
    let active = true;
    ordersApi.listLocations().then(
      (items) => active && setLocations(items),
      () => undefined,
    );
    return () => {
      active = false;
    };
  }, [ordersApi]);

  // AI-generated (edited by PENDING): where each supplier is, from
  // supplier-service. Without it every errand's distance is unknown and the
  // list stays newest first.
  useEffect(() => {
    let active = true;
    suppliersApi.listSuppliers().then(
      (suppliers) =>
        active &&
        setPickups(new Map(suppliers.map((s) => [s.id, { lat: s.latitude, lon: s.longitude }]))),
      () => undefined,
    );
    return () => {
      active = false;
    };
  }, [suppliersApi]);

  function setFilter(change: Partial<FindFilters>) {
    const next = { ...filters, ...change };
    setFilters(next);
    onSearch?.(next);
  }

  const others = list ? list.items.filter((errand) => errand.requesterId !== userId) : [];
  const ownHidden = list ? list.items.length - others.length : 0;
  // Decided by the server's actions alone, never by a rule worked out here.
  const takeable = others.filter((errand) => buttonsFor(errand).includes("accept"));
  const at = list ? serverNow(list.offset, now) : now;
  // AI-generated (edited by PENDING): what passes the courier's filters (D-092).
  const visible = filterErrands(takeable, filters, at, locations);
  // AI-generated (edited by PENDING): nearest pickup first once located (D-098).
  const from = sortOrder === "nearest" && courier.kind === "located" ? courier.at : null;
  const placed = rankByDistance(visible, from, pickups);
  const refusedCardShown = refusal !== null && visible.some((e) => e.id === refusal.errandId);
  // The visible text, which the button's name starts with.
  const acceptLabel = (id: string) =>
    accepting === id ? ACTION_BUSY_LABEL.accept : ACTION_LABEL.accept;

  // Accept loses focus while it is disabled. After a refusal it goes back to
  // that card's Accept, or, once the card has left the list (someone else
  // took the errand), to the message above the list, so a keyboard user is
  // not left at the top of the page.
  useEffect(() => {
    if (!refusal || accepting !== null) return;
    const lost = document.activeElement === null || document.activeElement === document.body;
    if (refusedCardShown) {
      if (refocusAccept.current && lost) acceptButtons.current.get(refusal.errandId)?.focus();
      refocusAccept.current = false;
    } else if (lost) {
      refusalRef.current?.focus();
    }
  }, [refusal, accepting, refusedCardShown]);

  return (
    <section>
      <div className="page-head">
        <h1 ref={headingRef} tabIndex={-1}>
          Find errands
        </h1>
        <p className="page-sub">
          {/* AI-generated (edited by PENDING): the order now depends on D-098. */}
          Errands other students have posted,{" "}
          {from ? "nearest pickup first" : "newest first"}. Accept one you can collect on a
          walk you&rsquo;re making anyway; its reward is yours once the requester confirms
          delivery.
        </p>
      </div>

      {/* AI-generated (edited by PENDING): asking for the courier's location
          (D-098) only when they tap, and saying why the order is what it is. */}
      {courier.kind === "ask" && (
        <p className="list-note">
          See the errands whose pickup is nearest you first.{" "}
          <button type="button" className="link-btn" onClick={requestLocation}>
            Use my location
          </button>
        </p>
      )}
      {courier.kind === "locating" && (
        <p className="list-note" role="status">
          Finding your location…
        </p>
      )}
      {courier.kind === "refused" && (
        <p className="list-note">
          Location access is off, so errands are newest first. You can allow it in your
          browser&rsquo;s settings for this site.
        </p>
      )}
      {courier.kind === "unavailable" && (
        <p className="list-note">Your location isn&rsquo;t available, so errands are newest first.</p>
      )}

      {error && (
        <p className="status error" role="alert">
          {list ? "Couldn't refresh the list. " : "Couldn't load open errands. "}
          {error}{" "}
          <button type="button" className="link-btn" onClick={refresh}>
            Retry
          </button>
        </p>
      )}

      {!list && !error && (
        <p className="status" role="status">
          Loading open errands…
        </p>
      )}

      {/* AI-generated (edited by PENDING): D-092's filters, laid out like the
          Suppliers screen's controls. */}
      {list && takeable.length > 0 && (
        <div className="controls" role="group" aria-label="Order and filter errands">
          {courier.kind === "located" && (
            <select
              className="category-select"
              value={sortOrder}
              onChange={(e) => setSortOrder(e.target.value as SortOrder)}
              aria-label="Order errands by"
            >
              <option value="nearest">Nearest pickup first</option>
              <option value="newest">Newest first</option>
            </select>
          )}
          <select
            className="category-select"
            value={filters.timeLeft}
            onChange={(e) => setFilter({ timeLeft: e.target.value as TimeLeft })}
            aria-label="Filter by time left"
          >
            {TIME_LEFT_OPTIONS.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </select>
          <select
            className="category-select"
            value={filters.from}
            onChange={(e) => setFilter({ from: e.target.value })}
            aria-label="Filter by where the errand is collected from"
          >
            <option value="">Collect from anywhere</option>
            {pickupOptions(takeable).map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
          <select
            className="category-select"
            value={filters.to}
            onChange={(e) => setFilter({ to: e.target.value })}
            aria-label="Filter by where the errand is delivered to"
          >
            <option value="">Deliver anywhere</option>
            {destinationGroups(locations).map((g) => (
              <optgroup key={g.area} label={g.area}>
                <option value={`area:${g.area}`}>All of {g.area}</option>
                {g.places.map((place) => (
                  <option key={place} value={`place:${place}`}>
                    {place}
                  </option>
                ))}
              </optgroup>
            ))}
          </select>
          {isFiltered(filters) && (
            <button type="button" className="link-btn" onClick={() => setFilter(NO_FILTERS)}>
              Clear filters
            </button>
          )}
        </div>
      )}

      {list && takeable.length === 0 && (
        <p className="status" role="status">
          No open errands right now. New ones show up here on their own.
        </p>
      )}

      {list && takeable.length > 0 && visible.length === 0 && (
        <p className="status" role="status">
          No open errands match these filters.{" "}
          <button type="button" className="link-btn" onClick={() => setFilter(NO_FILTERS)}>
            Clear filters
          </button>
        </p>
      )}

      {refusal && list && !refusedCardShown && (
        <p className="status error" tabIndex={-1} ref={refusalRef}>
          Couldn&rsquo;t accept &ldquo;{refusal.item}&rdquo;. {refusal.message}
        </p>
      )}

      {list && ownHidden > 0 && visible.length > 0 && (
        <p className="list-note">
          Your own open errands aren&rsquo;t listed here; they&rsquo;re in My errands under
          Requesting.
        </p>
      )}

      {placed.map(({ errand, metres }, position) => (
        <ErrandCard
          key={errand.id}
          cardRef={track(errand.id, position)}
          errand={errand}
          line={withDistance(progressText(errand, at), metres)}
          showState={false}
          onOpen={() =>
            setOpen({
              id: errand.id,
              initial: { errand, serverTime: new Date(at).toISOString() },
            })
          }
          note={
            refusal?.errandId === errand.id && (
              <p className="errand-card-note" role="alert">
                Couldn&rsquo;t accept. {refusal.message}
              </p>
            )
          }
        >
          <button
            type="button"
            className="btn btn-primary"
            ref={(node) => {
              if (node) acceptButtons.current.set(errand.id, node);
              else acceptButtons.current.delete(errand.id);
            }}
            onClick={() => void accept(errand)}
            disabled={accepting !== null}
            aria-label={`${acceptLabel(errand.id)}: ${errand.itemDescription}`}
          >
            {acceptLabel(errand.id)}
          </button>
        </ErrandCard>
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
            initialNotice={open.notice}
            ordersApi={ordersApi}
            suppliersApi={suppliersApi}
            usersApi={usersApi}
            viewerId={userId}
            viewerRole="STUDENT"
            available={available}
            onChanged={() => {
              refresh();
              onCreditsChanged();
            }}
            onGone={() => {
              // A courier who is not a party sees an errand only while it is
              // open, so a 404 here is an errand that ended or was taken, not
              // a failure: the neutral toast, in words that cover both.
              setOpen(null);
              onNotify({ text: MSG_NOT_OPEN_ANY_MORE, kind: "success" });
              refresh();
            }}
          />
        </Modal>
      )}
    </section>
  );
}

// AI-generated (edited by PENDING): the card's line with the pickup's distance (D-098).
/** The card's status line, with how far the pickup is when that is known. */
export function withDistance(line: string | null, metres: number | null): string | null {
  if (metres === null) return line;
  const away = `Pickup ${formatDistance(metres)} away`;
  return line ? `${line} · ${away}` : away;
}
