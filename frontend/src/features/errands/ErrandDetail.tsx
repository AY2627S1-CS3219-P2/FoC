// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-18
// Scope: An errand's detail, shown in a modal by every errand screen: its
//   fields, state and countdown, the other party (both for an admin, with
//   Look up account), its history with amended fields, the buttons from its
//   actions with their confirmation steps, what each action did in a status
//   line inside the dialog, and editing an OPEN errand, keeping the edits
//   when the errand changed under them. Polls while open.
// Author review: James Teo — reviewed and verified.

import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { usePolling } from "../../lib/polling";
import type { AccountRole } from "../auth/types";
import type { UserProfile, UsersApi } from "../profile/usersApi";
import type { SuppliersApi } from "../suppliers/suppliersApi";
import { stateBadgeClass } from "./ErrandCard";
import {
  ErrandForm,
  errandChanges,
  formChanged,
  formValuesFrom,
  keepEdits,
  type ErrandFormValues,
} from "./ErrandForm";
import {
  ACTION_BUSY_LABEL,
  ACTION_LABEL,
  actionDoneText,
  adminCancelDoneText,
  adminCancelStep,
  amendDoneText,
  confirmStepFor,
  creditsText,
  describeChange,
  detailLine,
  historyLabel,
  isErrandGone,
  isShortOfCredits,
  needsRefetch,
  orderErrorMessage,
  STATE_LABEL,
  type ConfirmStep,
} from "./format";
import { OrdersApiError, type OrdersApi } from "./ordersApi";
import {
  ERRAND_ACTIONS,
  type Errand,
  type ErrandAction,
  type ErrandDetail as ErrandDetailResponse,
  type HistoryEntry,
} from "./orderTypes";
import { clockOffset, formatDateTime, serverNow, useNow } from "./time";

/** The modal's heading id, for its aria-labelledby. One detail is open at a time. */
export const ERRAND_DETAIL_TITLE_ID = "errand-detail-title";

/** How the viewer stands to an errand, which decides who they are shown. */
export type Relationship = "requester" | "courier" | "admin" | "none";

export function relationshipOf(
  errand: Pick<Errand, "requesterId" | "courierId">,
  viewerId: string,
  viewerRole: AccountRole,
): Relationship {
  if (viewerRole === "ADMIN") return "admin";
  if (errand.requesterId && errand.requesterId === viewerId) return "requester";
  if (errand.courierId && errand.courierId === viewerId) return "courier";
  return "none";
}

/** A person on an errand the viewer is shown. */
export interface Party {
  role: "Requester" | "Courier";
  uid: string;
}

/**
 * Who the detail names: the courier to the requester once one is assigned,
 * the requester to the courier, both to an admin, and nobody to anyone else
 * (a courier browsing an OPEN errand). Names and contact numbers are shown
 * only here, never on list cards.
 */
export function partiesShown(
  errand: Pick<Errand, "requesterId" | "courierId">,
  relationship: Relationship,
): Party[] {
  const requester: Party[] = errand.requesterId ? [{ role: "Requester", uid: errand.requesterId }] : [];
  const courier: Party[] = errand.courierId ? [{ role: "Courier", uid: errand.courierId }] : [];
  switch (relationship) {
    case "requester":
      return courier;
    case "courier":
      return requester;
    case "admin":
      return [...requester, ...courier];
    default:
      return [];
  }
}

/** The order-service call behind each button, one named client function each. */
const ACTION_CALLS: Record<
  Exclude<ErrandAction, "amend">,
  (api: OrdersApi, errand: Errand) => Promise<ErrandDetailResponse>
> = {
  accept: (api, errand) => api.accept(errand.id, errand.version),
  withdraw: (api, errand) => api.withdraw(errand.id),
  pickup: (api, errand) => api.pickUp(errand.id),
  deliver: (api, errand) => api.deliver(errand.id),
  confirm: (api, errand) => api.confirm(errand.id),
  cancel: (api, errand) => api.cancel(errand.id),
  complete: (api, errand) => api.complete(errand.id),
};

/** The look of each action's button. */
export const ACTION_BUTTON_CLASS: Record<ErrandAction, string> = {
  amend: "btn-secondary",
  accept: "btn-primary",
  withdraw: "btn-secondary",
  pickup: "btn-primary",
  deliver: "btn-primary",
  confirm: "btn-primary",
  cancel: "btn-danger",
  complete: "btn-primary",
};

/**
 * The buttons an errand gets: its `actions` in the server's order, leaving
 * out any this build has no button for (a newer spec's).
 */
export function buttonsFor(errand: Pick<Errand, "actions">): ErrandAction[] {
  return errand.actions.filter((action) => ERRAND_ACTIONS.includes(action));
}

/** The errand as first drawn: a list's copy, before the detail arrives. */
export interface ErrandSnapshot {
  errand: Errand;
  serverTime: string;
  history?: HistoryEntry[];
}

interface ErrandDetailProps {
  errandId: string;
  /** Drawn at once while the detail and its history load. */
  initial?: ErrandSnapshot;
  ordersApi: OrdersApi;
  /** For the edit form's supplier picker. */
  suppliersApi: SuppliersApi;
  /** For the other party's name and contact; null in a fixture build. */
  usersApi: UsersApi | null;
  viewerId: string;
  viewerRole: AccountRole;
  /** The viewer's available credits, for the edit form's hint. */
  available: number | null;
  /** After any action succeeds: the list and the balance refetch. */
  onChanged: () => void;
  /**
   * After the requester lowers the reward: credit-service releases the
   * difference a moment after the save, so the balance is fetched again until
   * it shows (D-086).
   */
  onRewardLowered?: () => void;
  /** The errand answered 404: the modal closes with this message. */
  onGone: (message: string) => void;
  /**
   * What the action that opened the detail did, such as Accept on a list
   * card, shown in the detail's status line.
   */
  initialNotice?: string;
  /** Admin only: opens the Admin screen with that account looked up. */
  onLookUpAccount?: (uid: string) => void;
  /** Opens Credits, from the not-enough-credits message. */
  onOpenCredits?: () => void;
}

/** What the detail last heard from order-service. */
interface Loaded {
  errand: Errand;
  /** Null until the first GET arrives with it. */
  history: HistoryEntry[] | null;
  /** Server clock minus device clock when it arrived. */
  offset: number;
}

interface EditFailure {
  message: string;
  /** 409: the errand changed since the form was filled; offer to refresh it. */
  stale: boolean;
  shortOfCredits: boolean;
}

const MSG_NOTHING_CHANGED = "Nothing has changed yet. Change a field, or discard the edit.";
const MSG_EDITS_KEPT = "Your changes are kept. Check them and save again.";
const MSG_LATEST_LOADED = "This is the latest version of the errand.";

/**
 * Every button comes from the errand's `actions`; the detail never works out
 * a transition. While any call is in flight every button is disabled (F7.2),
 * and after any answer the detail redraws from the errand it got back.
 */
export function ErrandDetail({
  errandId,
  initial,
  ordersApi,
  suppliersApi,
  usersApi,
  viewerId,
  viewerRole,
  available,
  onChanged,
  onRewardLowered,
  onGone,
  initialNotice,
  onLookUpAccount,
  onOpenCredits,
}: ErrandDetailProps) {
  const [loaded, setLoaded] = useState<Loaded | null>(() =>
    initial
      ? {
          errand: initial.errand,
          history: initial.history ?? null,
          offset: clockOffset(initial.serverTime),
        }
      : null,
  );
  const [loadError, setLoadError] = useState<string | null>(null);
  const [busy, setBusy] = useState<ErrandAction | null>(null);
  const [confirming, setConfirming] = useState<ErrandAction | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  // What the last action did. Shown in a status line inside the dialog, since
  // a toast outside it may not be read while the dialog is open.
  const [notice, setNotice] = useState<string | null>(null);
  const [editBase, setEditBase] = useState<Errand | null>(null);
  const [editFailure, setEditFailure] = useState<EditFailure | null>(null);
  // A note in the edit form that is not a failure, such as nothing to save.
  const [editNote, setEditNote] = useState<string | null>(null);
  // The edit form's boxes as they stand, so refreshing after a 409 keeps them.
  const editValues = useRef<ErrandFormValues | null>(null);
  const rememberEditValues = useCallback((values: ErrandFormValues) => {
    editValues.current = values;
  }, []);
  // Kept edits the refreshed form opens with, over the latest errand.
  const [keptValues, setKeptValues] = useState<ErrandFormValues | null>(null);
  const [parties, setParties] = useState<Record<string, UserProfile | "failed">>({});

  const titleRef = useRef<HTMLHeadingElement>(null);
  const buttonRefs = useRef(new Map<ErrandAction, HTMLButtonElement>());
  const [returnFocusTo, setReturnFocusTo] = useState<ErrandAction | null>(null);
  const now = useNow(1000);

  function apply(detail: ErrandDetailResponse) {
    setLoaded({
      errand: detail.errand,
      history: detail.history,
      offset: clockOffset(detail.serverTime),
    });
  }

  const refresh = usePolling(
    async (isActive) => {
      try {
        const detail = await ordersApi.getErrand(errandId);
        if (!isActive()) return;
        apply(detail);
        setLoadError(null);
      } catch (err) {
        if (isActive()) {
          if (isErrandGone(err)) onGone(orderErrorMessage(err));
          else setLoadError(orderErrorMessage(err));
        }
        throw err;
      }
    },
    5_000,
    [ordersApi, errandId],
  );

  const errand = loaded?.errand ?? null;
  const relationship = errand ? relationshipOf(errand, viewerId, viewerRole) : "none";
  const shownParties = errand ? partiesShown(errand, relationship) : [];
  const partyKey = shownParties.map((party) => party.uid).join(",");

  // The other party's username and contact, from user-service. Fetched again
  // only when who is shown changes (a courier assigned or gone).
  useEffect(() => {
    if (!usersApi || !partyKey) return;
    let cancelled = false;
    for (const uid of partyKey.split(",")) {
      usersApi.getUser(uid).then(
        (profile) => {
          if (!cancelled) setParties((prev) => ({ ...prev, [uid]: profile }));
        },
        () => {
          if (!cancelled) setParties((prev) => ({ ...prev, [uid]: "failed" }));
        },
      );
    }
    return () => {
      cancelled = true;
    };
  }, [usersApi, partyKey]);

  // The status line is on the page, empty, before the opening action's text
  // goes into it, so a screen reader reads that text out.
  useEffect(() => {
    if (!initialNotice) return;
    const timer = setTimeout(() => setNotice(initialNotice), 100);
    return () => clearTimeout(timer);
  }, [initialNotice]);

  // A confirmation step replaces the button row, so focus goes back to the
  // button that opened it once it closes, rather than being lost.
  useEffect(() => {
    if (confirming || !returnFocusTo) return;
    buttonRefs.current.get(returnFocusTo)?.focus();
    setReturnFocusTo(null);
  }, [confirming, returnFocusTo]);

  if (!loaded || !errand) {
    return (
      <>
        <h2 className="detail-title" id={ERRAND_DETAIL_TITLE_ID}>
          Errand
        </h2>
        {loadError ? (
          <p className="status error" role="alert">
            {loadError}{" "}
            <button type="button" className="link-btn" onClick={refresh}>
              Retry
            </button>
          </p>
        ) : (
          <p className="status" role="status">
            Loading the errand…
          </p>
        )}
      </>
    );
  }

  const at = serverNow(loaded.offset, now);
  const buttons = buttonsFor(errand);
  // What happens next, worded for the viewer, so it reads even with no button.
  const line = detailLine(errand, relationship, at);

  // The step for the action being confirmed, while the errand still offers it.
  const step: ConfirmStep | null =
    confirming && buttons.includes(confirming)
      ? confirming === "cancel" && relationship === "admin"
        ? adminCancelStep(errand)
        : confirmStepFor(confirming, errand)
      : null;

  function press(action: ErrandAction) {
    setFailure(null);
    setNotice(null);
    if (action === "amend") {
      setEditFailure(null);
      setEditNote(null);
      setKeptValues(null);
      setEditBase(errand);
      return;
    }
    const needsStep =
      (action === "cancel" && relationship === "admin") || confirmStepFor(action, errand!) !== null;
    if (needsStep) setConfirming(action);
    else void run(action);
  }

  function goBack() {
    setReturnFocusTo(confirming);
    setConfirming(null);
  }

  async function run(action: ErrandAction) {
    if (action === "amend" || !errand) return;
    const before = errand;
    setBusy(action);
    setFailure(null);
    setNotice(null);
    try {
      apply(await ACTION_CALLS[action](ordersApi, before));
    } catch (err) {
      setConfirming(null);
      setBusy(null);
      if (isErrandGone(err)) {
        onGone(orderErrorMessage(err));
        return;
      }
      setFailure(orderErrorMessage(err));
      if (needsRefetch(err)) refresh();
      // The pressed button lost focus when it was disabled; keep it in the dialog.
      titleRef.current?.focus();
      return;
    }
    // A poll sent before the action could answer after it with the old
    // errand; refresh() drops that answer and asks again.
    refresh();
    setConfirming(null);
    setBusy(null);
    setNotice(
      action === "cancel" && relationship === "admin"
        ? adminCancelDoneText(before)
        : actionDoneText(action, before),
    );
    onChanged();
    // The pressed button may be gone from the new errand's row.
    titleRef.current?.focus();
  }

  async function saveEdit(values: ErrandFormValues) {
    if (!editBase) return;
    const changes = errandChanges(editBase, values);
    if (Object.keys(changes).length === 0) {
      setEditFailure(null);
      setEditNote(MSG_NOTHING_CHANGED);
      return;
    }
    setBusy("amend");
    setEditFailure(null);
    setEditNote(null);
    setNotice(null);
    let saved: Pick<Errand, "reward"> = editBase;
    try {
      const detail = await ordersApi.amendErrand(editBase.id, editBase.version, changes);
      apply(detail);
      saved = detail.errand;
    } catch (err) {
      setBusy(null);
      if (isErrandGone(err)) {
        onGone(orderErrorMessage(err));
        return;
      }
      setEditFailure({
        message: orderErrorMessage(err),
        stale: err instanceof OrdersApiError && err.status === 409,
        shortOfCredits: isShortOfCredits(err),
      });
      // ErrandForm puts focus back on Save beside this message.
      if (needsRefetch(err)) refresh();
      return;
    }
    // As after an action: drop a poll answer from before the save.
    refresh();
    setBusy(null);
    setEditBase(null);
    setNotice(amendDoneText(editBase, saved));
    onChanged();
    if (saved.reward < editBase.reward) onRewardLowered?.();
    titleRef.current?.focus();
  }

  /**
   * After a 409 on save: fetch the errand again and refill the form from it,
   * keeping each field the student had changed (keepEdits).
   */
  async function reloadForEdit() {
    const before = editBase;
    const typed = editValues.current;
    setEditFailure(null);
    setEditNote(null);
    try {
      const detail = await ordersApi.getErrand(errandId);
      apply(detail);
      if (detail.errand.actions.includes("amend")) {
        const latest = formValuesFrom(detail.errand);
        const kept = before && typed ? keepEdits(formValuesFrom(before), typed, latest) : latest;
        const keeping = formChanged(latest, kept);
        setKeptValues(keeping ? kept : null);
        setEditBase(detail.errand);
        setEditNote(keeping ? MSG_EDITS_KEPT : MSG_LATEST_LOADED);
      } else {
        setEditBase(null);
        setFailure(
          `This errand is now ${STATE_LABEL[detail.errand.state].toLowerCase()}, so it can't be edited.`,
        );
      }
    } catch (err) {
      if (isErrandGone(err)) {
        onGone(orderErrorMessage(err));
        return;
      }
      setEditFailure({ message: orderErrorMessage(err), stale: true, shortOfCredits: false });
    }
  }

  return (
    <>
      <h2 className="detail-title" id={ERRAND_DETAIL_TITLE_ID} tabIndex={-1} ref={titleRef}>
        {errand.itemDescription}
      </h2>
      <div className="detail-status">
        <span className={`badge ${stateBadgeClass(errand.state)}`}>
          {STATE_LABEL[errand.state] ?? errand.state}
        </span>
        {line && <span className="detail-deadline">{line}</span>}
      </div>

      {/* On the page from the first draw, empty until an action succeeds. */}
      <p className="detail-notice" role="status">
        {notice}
      </p>

      {loadError && (
        <p className="status error">
          Couldn&rsquo;t refresh this errand: {loadError}{" "}
          <button type="button" className="link-btn" onClick={refresh}>
            Retry
          </button>
        </p>
      )}

      {editBase ? (
        <ErrandForm
          key={`${editBase.id}:${editBase.version}`}
          suppliersApi={suppliersApi}
          ordersApi={ordersApi}
          initial={formValuesFrom(editBase)}
          startWith={keptValues ?? undefined}
          onValuesChange={rememberEditValues}
          alreadyHeld={editBase.reward}
          available={available}
          notice="Editing doesn't move the expiry time."
          submitting={busy === "amend"}
          submitLabel={() => "Save changes"}
          busyLabel="Saving…"
          cancelLabel="Discard changes"
          onSubmit={saveEdit}
          onCancel={() => {
            setEditBase(null);
            setEditFailure(null);
            setEditNote(null);
            setKeptValues(null);
            setReturnFocusTo("amend");
          }}
          feedback={
            <>
              {/* On the page from the first draw, so a screen reader hears it change. */}
              <p className="detail-notice" role="status">
                {editNote}
              </p>
              {editFailure && (
                <p className="status error" role="alert">
                  {editFailure.message}
                  {editFailure.stale && (
                    <>
                      {" "}
                      <button type="button" className="link-btn" onClick={reloadForEdit}>
                        Refresh errand
                      </button>
                    </>
                  )}
                  {editFailure.shortOfCredits && onOpenCredits && (
                    <>
                      {" "}
                      <button type="button" className="link-btn" onClick={onOpenCredits}>
                        See your credits
                      </button>
                    </>
                  )}
                </p>
              )}
            </>
          }
        />
      ) : (
        <>
          <dl className="detail-list errand-facts">
            <Row label="Collect from">
              {errand.supplier.location
                ? `${errand.supplier.name} · ${errand.supplier.location}`
                : errand.supplier.name}
            </Row>
            <Row label="Deliver to">{errand.deliveryLocation}</Row>
            {errand.deliveryNote && (
              <Row
                label={relationship === "requester" ? "How to find you" : "How to find the requester"}
                multiline
              >
                {errand.deliveryNote}
              </Row>
            )}
            <Row label="Reward">{creditsText(errand.reward)}</Row>
            {shownParties.map((party) => (
              <Row key={party.uid} label={party.role}>
                <PartyDetails
                  party={party}
                  profile={parties[party.uid]}
                  showAccount={relationship === "admin"}
                  hasDirectory={usersApi !== null}
                  onLookUpAccount={onLookUpAccount}
                />
              </Row>
            ))}
            <Row label="Posted">{formatDateTime(errand.createdAt)}</Row>
          </dl>

          {failure && (
            <p className="status error" role="alert">
              {failure}
            </p>
          )}

          {step && confirming ? (
            <div
              className={`confirm-box ${step.tone === "neutral" ? "neutral" : ""}`}
              role="group"
              aria-labelledby="errand-confirm-question"
            >
              <p id="errand-confirm-question">
                <strong>{step.question}</strong> {step.consequence}
              </p>
              <div className="btn-row start">
                <button
                  type="button"
                  className={`btn ${step.tone === "danger" ? "btn-danger" : "btn-primary"}`}
                  onClick={() => void run(confirming)}
                  disabled={busy !== null}
                >
                  {busy === confirming ? ACTION_BUSY_LABEL[confirming] : step.confirmLabel}
                </button>
                <button
                  type="button"
                  className="btn btn-secondary"
                  onClick={goBack}
                  disabled={busy !== null}
                  autoFocus
                >
                  Go back
                </button>
              </div>
            </div>
          ) : (
            buttons.length > 0 && (
              <div className="btn-row start detail-actions">
                {buttons.map((action) => (
                  <button
                    key={action}
                    type="button"
                    ref={(node) => {
                      if (node) buttonRefs.current.set(action, node);
                      else buttonRefs.current.delete(action);
                    }}
                    className={`btn ${ACTION_BUTTON_CLASS[action]}`}
                    onClick={() => press(action)}
                    disabled={busy !== null}
                  >
                    {busy === action ? ACTION_BUSY_LABEL[action] : ACTION_LABEL[action]}
                  </button>
                ))}
              </div>
            )
          )}

          <h3 className="section-label">History</h3>
          {loaded.history === null ? (
            <p className="status" role="status">
              Loading history…
            </p>
          ) : (
            <ol className="timeline">
              {loaded.history.map((entry, index) => (
                <li key={`${entry.occurredAt}-${index}`}>
                  <span className="timeline-what">{historyLabel(entry.action)}</span>{" "}
                  <time className="timeline-when" dateTime={entry.occurredAt}>
                    {formatDateTime(entry.occurredAt)}
                  </time>
                  {entry.changes && entry.changes.length > 0 && (
                    <ul className="timeline-changes">
                      {entry.changes.map((change, i) => (
                        <li key={i}>{describeChange(change)}</li>
                      ))}
                    </ul>
                  )}
                </li>
              ))}
            </ol>
          )}
        </>
      )}
    </>
  );
}

function Row({
  label,
  multiline = false,
  children,
}: {
  label: string;
  multiline?: boolean;
  children: ReactNode;
}) {
  return (
    <div>
      <dt>{label}</dt>
      <dd className={multiline ? "multiline" : undefined}>{children}</dd>
    </div>
  );
}

interface PartyDetailsProps {
  party: Party;
  /** Undefined while loading. */
  profile: UserProfile | "failed" | undefined;
  /** An admin sees the account ID and can look the account up. */
  showAccount: boolean;
  /** False in a fixture build, which has no user-service to ask. */
  hasDirectory: boolean;
  onLookUpAccount?: (uid: string) => void;
}

/**
 * Username and contact number, the number as a link that calls it, and for an
 * admin the account ID and Look up account.
 */
function PartyDetails({
  party,
  profile,
  showAccount,
  hasDirectory,
  onLookUpAccount,
}: PartyDetailsProps) {
  let who: ReactNode;
  if (!hasDirectory) who = null;
  else if (profile === undefined) who = <span>Loading…</span>;
  else if (profile === "failed") who = <span>Couldn&rsquo;t load their details</span>;
  else
    who = (
      <>
        <span>{profile.username || "—"}</span>
        {profile.contact ? (
          <a
            className="party-contact"
            href={`tel:${profile.contact.replace(/[^\d+]/g, "")}`}
            aria-label={`Call ${profile.contact}`}
          >
            {profile.contact}
          </a>
        ) : (
          <span className="party-none">No contact number yet</span>
        )}
      </>
    );

  return (
    <span className="party">
      {who}
      {showAccount && (
        <>
          <span className="party-id">{party.uid}</span>
          {onLookUpAccount && (
            <button
              type="button"
              className="link-btn"
              onClick={() => onLookUpAccount(party.uid)}
            >
              Look up account
              <span className="visually-hidden"> of the {party.role.toLowerCase()}</span>
            </button>
          )}
        </>
      )}
    </span>
  );
}
