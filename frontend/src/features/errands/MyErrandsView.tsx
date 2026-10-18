// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Built the My errands list against the mock order-service.
// Author review: Nigeltzy - AI built it according to the prototype our team designed and the requirements provided. The code  is fairly boilerplate, seems valid.

// AI-generated (edited by James Teo): rebuilt on order-service. The mode picks
// the requester's or the courier's lists, each with Active (polled) and
// History, and an errand opens its detail in a modal.
import { useEffect, useRef, useState } from "react";
import { Modal } from "../../components/Modal";
import type { ToastMessage } from "../../components/Toast";
import { usePolling } from "../../lib/polling";
import type { ActingMode } from "../auth/types";
import type { UsersApi } from "../profile/usersApi";
import type { SuppliersApi } from "../suppliers/suppliersApi";
import type { ViewName } from "../../views";
import { ErrandCard, RewardText } from "./ErrandCard";
import { ERRAND_DETAIL_TITLE_ID, ErrandDetail, type ErrandSnapshot } from "./ErrandDetail";
import {
  canOpenAssignment,
  confirmByText,
  OUTCOME_LABEL,
  orderErrorMessage,
  progressText,
} from "./format";
import type { OrdersApi } from "./ordersApi";
import type { AssignmentOutcome, AssignmentRecord, Errand, ListScope } from "./orderTypes";
import { clockOffset, formatDateTime, serverNow, useNow } from "./time";

interface MyErrandsViewProps {
  // AI-generated (edited by James Teo): the mode picks the lists, and the
  // clients and session details the detail needs are passed in.
  /** Picks the lists: errands you posted, or errands you carry as courier. */
  mode: ActingMode;
  /** order-service's client, built by App over the authorized transport. */
  ordersApi: OrdersApi;
  /** For the edit form's supplier picker. */
  suppliersApi: SuppliersApi;
  /** For the other party's name and contact in the detail. */
  usersApi: UsersApi | null;
  /** The signed-in student. */
  userId: string;
  available: number | null;
  onNotify: (message: ToastMessage) => void;
  /** After an action in the detail, so App refreshes the balance. */
  onCreditsChanged: () => void;
  /** For the empty states' buttons and the Credits link. */
  onNavigate: (view: ViewName) => void;
}

// AI-generated (edited by James Teo): the lists as last fetched.

/** Errands with the server clock's offset when they arrived. */
interface ErrandRows {
  kind: "errands";
  items: Errand[];
  offset: number;
}

/** A courier's ended assignments. */
interface AssignmentRows {
  kind: "assignments";
  records: AssignmentRecord[];
}

type Loaded = ErrandRows | AssignmentRows;

/** Badge colours for an assignment's outcome; the badge carries its name. */
const OUTCOME_BADGE: Record<AssignmentOutcome, string> = {
  COMPLETED: "badge-available",
  CANCELLED: "badge-unavailable",
  RELEASED: "badge-type",
};

export function MyErrandsView({
  mode,
  ordersApi,
  suppliersApi,
  usersApi,
  userId,
  available,
  onNotify,
  onCreditsChanged,
  onNavigate,
}: MyErrandsViewProps) {
  // AI-generated (edited by James Teo): Active is polled while shown; History
  // is fetched each time it is opened and not polled.
  const [scope, setScope] = useState<ListScope>("active");
  const [active, setActive] = useState<ErrandRows | null>(null);
  const [activeError, setActiveError] = useState<string | null>(null);
  const [history, setHistory] = useState<Loaded | null>(null);
  const [historyError, setHistoryError] = useState<string | null>(null);
  const [historyAttempt, setHistoryAttempt] = useState(0);
  const [open, setOpen] = useState<{ id: string; initial?: ErrandSnapshot } | null>(null);
  const now = useNow(1000);
  const requesting = mode === "requesting";
  // AI-generated (edited by James Teo): takes focus when a closed detail's
  // card has left the list, as after a cancel.
  const headingRef = useRef<HTMLHeadingElement>(null);

  const refreshActive = usePolling(
    scope === "active"
      ? async (isActive) => {
          try {
            const result = requesting
              ? await ordersApi.listRequested("active")
              : await ordersApi.listAssigned("active");
            if (!isActive()) return;
            setActive({
              kind: "errands",
              items: result.items,
              offset: clockOffset(result.serverTime),
            });
            setActiveError(null);
          } catch (err) {
            if (isActive()) setActiveError(orderErrorMessage(err));
            throw err;
          }
        }
      : null,
    5_000,
    [ordersApi, requesting],
  );

  useEffect(() => {
    if (scope !== "history") return;
    let cancelled = false;
    setHistoryError(null);
    const load: Promise<Loaded> = requesting
      ? ordersApi.listRequested("history").then((result) => ({
          kind: "errands",
          items: result.items,
          offset: clockOffset(result.serverTime),
        }))
      : ordersApi
          .listAssigned("history")
          .then((result) => ({ kind: "assignments", records: result.history }));
    load.then(
      (loaded) => {
        if (!cancelled) setHistory(loaded);
      },
      (err: unknown) => {
        if (!cancelled) setHistoryError(orderErrorMessage(err));
      },
    );
    return () => {
      cancelled = true;
    };
  }, [scope, historyAttempt, ordersApi, requesting]);

  /** After an action in the detail: whichever list is showing, and the balance. */
  function changed() {
    if (scope === "active") refreshActive();
    else setHistoryAttempt((n) => n + 1);
    onCreditsChanged();
  }

  function openErrand(errand: Errand, offset: number) {
    setOpen({
      id: errand.id,
      initial: { errand, serverTime: new Date(serverNow(offset, now)).toISOString() },
    });
  }

  function showScope(next: ListScope) {
    // History is fetched afresh each time it is opened, so its old rows are
    // not shown as if they were current.
    if (next === "history" && scope !== "history") setHistory(null);
    setScope(next);
  }

  const shown = scope === "active" ? active : history;
  const error = scope === "active" ? activeError : historyError;
  const retry = scope === "active" ? refreshActive : () => setHistoryAttempt((n) => n + 1);
  const empty =
    shown !== null &&
    (shown.kind === "errands" ? shown.items.length === 0 : shown.records.length === 0);

  return (
    <section>
      <div className="page-head">
        {/* AI-generated (edited by James Teo): focusable, for the detail's
            fallback focus. */}
        <h1 ref={headingRef} tabIndex={-1}>
          My errands
        </h1>
        <p className="page-sub">
          {requesting
            ? "Errands you have posted. Credits stay held until one is completed, cancelled or expires."
            : "Errands you have taken on as a courier."}
        </p>
      </div>

      {/* AI-generated (edited by James Teo): the Active / History toggle, then
          the loading, failed, empty and loaded states of the list shown. */}
      <div className="mode-toggle list-scope" role="group" aria-label="Which errands to show">
        <button
          type="button"
          className={scope === "active" ? "active" : ""}
          aria-pressed={scope === "active"}
          onClick={() => showScope("active")}
        >
          Active
        </button>
        <button
          type="button"
          className={scope === "history" ? "active" : ""}
          aria-pressed={scope === "history"}
          onClick={() => showScope("history")}
        >
          History
        </button>
      </div>

      {error && (
        <p className="status error" role="alert">
          {shown ? "Couldn't refresh this list. " : "Couldn't load your errands. "}
          {error}{" "}
          <button type="button" className="link-btn" onClick={retry}>
            Retry
          </button>
        </p>
      )}

      {!shown && !error && (
        <p className="status" role="status">
          Loading errands…
        </p>
      )}

      {empty && (
        <div className="status empty-state" role="status">
          {scope === "history" ? (
            <p>{requesting ? "No finished errands yet." : "No finished deliveries yet."}</p>
          ) : requesting ? (
            <>
              <p>Nothing in progress.</p>
              <button
                type="button"
                className="btn btn-primary"
                onClick={() => onNavigate("new-errand")}
              >
                Post an errand
              </button>
            </>
          ) : (
            <>
              <p>You&rsquo;re not carrying anything.</p>
              <button
                type="button"
                className="btn btn-primary"
                onClick={() => onNavigate("find-errands")}
              >
                Find errands
              </button>
            </>
          )}
        </div>
      )}

      {/* AI-generated (edited by James Teo): cards read order-service's fields
          and open the errand's detail. A delivery waiting on the requester's
          confirmation says so on its card. */}
      {shown?.kind === "errands" &&
        shown.items.map((errand) => {
          const at = serverNow(shown.offset, now);
          const toConfirm = errand.actions.includes("confirm");
          return (
            <ErrandCard
              key={errand.id}
              errand={errand}
              line={(toConfirm ? confirmByText(errand, at) : null) ?? progressText(errand, at)}
              needsYou={toConfirm}
              onOpen={() => openErrand(errand, shown.offset)}
            />
          );
        })}

      {shown?.kind === "assignments" &&
        shown.records.map((record) => (
          <AssignmentRow
            key={`${record.errandId}-${record.endedAt}`}
            record={record}
            onOpen={() => setOpen({ id: record.errandId })}
          />
        ))}

      {/* AI-generated (edited by James Teo): the errand's detail, which says
          inside itself what an action did. */}
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
            viewerRole="STUDENT"
            available={available}
            onChanged={changed}
            onGone={(message) => {
              setOpen(null);
              onNotify({ text: message, kind: "error" });
              changed();
            }}
            onOpenCredits={() => onNavigate("credits")}
          />
        </Modal>
      )}
    </section>
  );
}

// AI-generated (edited by James Teo): one ended assignment in a courier's
// history.
/**
 * How and when it ended, with the errand's supplier, item, location and
 * reward. A completed or cancelled one opens the detail, since the courier is
 * still its party; a released one cannot be opened (canOpenAssignment).
 */
function AssignmentRow({ record, onOpen }: { record: AssignmentRecord; onOpen: () => void }) {
  const ended = formatDateTime(record.endedAt);
  const outcome = OUTCOME_LABEL[record.outcome] ?? record.outcome;
  const body = (
    <>
      <span className="errand-card-body">
        <span className="errand-title">{record.itemDescription}</span>
        <span className="errand-meta">
          {record.supplier.name} → {record.deliveryLocation}
        </span>
        <span className="errand-line">
          {outcome}
          {ended ? ` ${ended}` : ""}
          {record.outcome === "RELEASED" ? " · you withdrew, so it can't be opened" : ""}
        </span>
      </span>
      <span className="errand-card-side">
        <span className={`badge ${OUTCOME_BADGE[record.outcome] ?? "badge-type"}`}>{outcome}</span>
        <RewardText reward={record.reward} muted={record.outcome !== "COMPLETED"} />
      </span>
    </>
  );

  return (
    <article className="errand-card has-open">
      {canOpenAssignment(record) ? (
        <button type="button" className="errand-card-open" onClick={onOpen}>
          {body}
        </button>
      ) : (
        <div className="errand-card-open static">{body}</div>
      )}
    </article>
  );
}
