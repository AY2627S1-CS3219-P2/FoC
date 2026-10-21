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
import type { OrdersApi } from "./ordersApi";
import type { Errand } from "./orderTypes";
import { clockOffset, serverNow, useNow } from "./time";

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
}: FindErrandsViewProps) {
  const [list, setList] = useState<OpenList | null>(null);
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

  const others = list ? list.items.filter((errand) => errand.requesterId !== userId) : [];
  const ownHidden = list ? list.items.length - others.length : 0;
  // Decided by the server's actions alone, never by a rule worked out here.
  const takeable = others.filter((errand) => buttonsFor(errand).includes("accept"));
  const at = list ? serverNow(list.offset, now) : now;
  const refusedCardShown = refusal !== null && takeable.some((e) => e.id === refusal.errandId);
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
          Errands other students have posted, newest first. Accept one you can collect on a
          walk you&rsquo;re making anyway; its reward is yours once the requester confirms
          delivery.
        </p>
      </div>

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

      {list && takeable.length === 0 && (
        <p className="status" role="status">
          No open errands right now. New ones show up here on their own.
        </p>
      )}

      {refusal && list && !refusedCardShown && (
        <p className="status error" tabIndex={-1} ref={refusalRef}>
          Couldn&rsquo;t accept &ldquo;{refusal.item}&rdquo;. {refusal.message}
        </p>
      )}

      {list && ownHidden > 0 && takeable.length > 0 && (
        <p className="list-note">
          Your own open errands aren&rsquo;t listed here; they&rsquo;re in My errands under
          Requesting.
        </p>
      )}

      {takeable.map((errand) => (
        <ErrandCard
          key={errand.id}
          errand={errand}
          line={progressText(errand, at)}
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
