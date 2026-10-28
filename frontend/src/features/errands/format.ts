// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-18
// Scope: Display text for errands: state, history-action, assignment-outcome
//   and amended-field labels, the deadline, progress, ended and time-in-state
//   lines, the detail's line for each viewer, the action buttons with their
//   confirmation steps and success messages, and the message a screen shows
//   for each kind of failed order-service call.
// Author review: James Teo — reviewed and verified.

import { OrdersApiError } from "./ordersApi";
import type {
  AssignmentOutcome,
  AssignmentRecord,
  ChangedField,
  Errand,
  ErrandAction,
  ErrandStatus,
  FieldChange,
  HistoryAction,
} from "./orderTypes";
import { countdown, elapsed, formatDateTime } from "./time";

/**
 * Presentation only. Nothing here decides what an errand allows: buttons come
 * from the server's `actions`, and a failed call is the server's refusal,
 * reported in words.
 */

/** Display text for each errand state. */
export const STATE_LABEL: Record<ErrandStatus, string> = {
  // AI Assistance Disclosure: Codex (GPT-6), 2026-10-28 — label for order-service's scheduled state.
  SCHEDULED: "Scheduled",
  OPEN: "Open",
  ACCEPTED: "Accepted",
  PICKED_UP: "Picked up",
  DELIVERED: "Delivered",
  COMPLETED: "Completed",
  CANCELLED: "Cancelled",
  EXPIRED: "Expired",
};

const HISTORY_LABEL: Record<HistoryAction, string> = {
  CREATE: "Posted",
  AMEND: "Edited",
  // AI Assistance Disclosure: Codex (GPT-6), 2026-10-28 — label for the system OPEN history action.
  OPEN: "Opened",
  ACCEPT: "Accepted",
  WITHDRAW: "Courier withdrew",
  PICK_UP: "Picked up",
  DELIVER: "Delivered",
  CONFIRM: "Delivery confirmed",
  AUTO_CONFIRM: "Confirmed automatically",
  CANCEL: "Cancelled",
  EXPIRE: "Expired",
  ADMIN_CANCEL: "Cancelled by an admin",
  ADMIN_COMPLETE: "Completed by an admin",
};

/**
 * The label for a history entry's action. An action this build does not know
 * yet (the spec gained one) is shown as its own words rather than left blank.
 */
export function historyLabel(action: string): string {
  return HISTORY_LABEL[action as HistoryAction] ?? sentence(action.replace(/_/g, " ").toLowerCase(), "");
}

/** How a courier's assignment ended, for their assignment history. */
export const OUTCOME_LABEL: Record<AssignmentOutcome, string> = {
  COMPLETED: "Completed",
  CANCELLED: "Cancelled",
  RELEASED: "Released",
};

const FIELD_LABEL: Record<ChangedField, string> = {
  supplier: "Supplier",
  itemDescription: "Item",
  deliveryLocation: "Delivery location",
  deliveryNote: "Note",
  reward: "Reward",
  // AI Assistance Disclosure: Codex (GPT-6), 2026-10-28 — label for recorded schedule amendments.
  scheduled: "Scheduled time",
};

/**
 * One amended field as a history line, e.g. "Reward: 10 → 15". An empty side
 * reads "none" (a note added or cleared). order-service sends a note change
 * to the courier with both sides empty, since they may not read the note:
 * that reads "Note changed".
 */
export function describeChange(change: FieldChange): string {
  const label = FIELD_LABEL[change.field] ?? change.field;
  if (change.from === "" && change.to === "") return `${label} changed`;
  return `${label}: ${change.from || "none"} → ${change.to || "none"}`;
}

/**
 * The countdown line for an errand at `now` (server time): "Expires in
 * 1 h 52 min" while it has an `expiresAt`, "Confirms itself in 23 h" while it
 * has a `confirmBy`, and "Expiring…" or "Confirming…" once that time is
 * reached (F3.2.3, F3.4.2). Null when the errand has neither deadline.
 */
export function deadlineText(
  errand: Pick<Errand, "expiresAt" | "confirmBy">,
  now: number,
): string | null {
  const expiry = countdown(errand.expiresAt, now);
  if (expiry) return expiry.due ? "Expiring…" : `Expires in ${expiry.text}`;
  const confirmation = countdown(errand.confirmBy, now);
  if (confirmation) {
    return confirmation.due ? "Confirming…" : `Confirms itself in ${confirmation.text}`;
  }
  return null;
}

/**
 * The line under an errand on its requester's or courier's list: the
 * countdown while one runs (deadlineText), otherwise what it is waiting for,
 * or for a final errand, when it ended ("Completed 18 Oct, 17:06").
 */
export function progressText(
  errand: Pick<Errand, "state" | "expiresAt" | "confirmBy" | "stateChangedAt">,
  now: number,
): string {
  const deadline = deadlineText(errand, now);
  if (deadline) return deadline;
  switch (errand.state) {
    case "OPEN":
      return "Waiting for a courier";
    case "ACCEPTED":
      return "Waiting to be picked up";
    case "PICKED_UP":
      return "On the way";
    case "DELIVERED":
      return "Waiting for the requester to confirm";
    default:
      return endedText(errand);
  }
}

/**
 * When an errand reached its state, by the state's label: "Cancelled
 * 18 Oct, 20:38", or the label alone when the time cannot be read. For a
 * final errand this is when it ended.
 */
export function endedText(errand: Pick<Errand, "state" | "stateChangedAt">): string {
  const when = formatDateTime(errand.stateChangedAt);
  return when ? `${STATE_LABEL[errand.state]} ${when}` : STATE_LABEL[errand.state];
}

const FINAL_STATES: readonly ErrandStatus[] = ["COMPLETED", "CANCELLED", "EXPIRED"];

/**
 * The line beside the state badge in an errand's detail, worded for the
 * viewer, so one with no button to press still reads what happens next:
 * - a final errand, to anyone: when it ended (endedText);
 * - an OPEN errand: its countdown (deadlineText);
 * - the requester: who has it, where it is going, and when to confirm by;
 * - the courier: the next step, and once delivered, when they are paid;
 * - an admin: the countdown while one runs, else how long it has been in
 *   its state.
 * `viewer` is how the viewer stands to the errand (ErrandDetail's
 * relationshipOf); `now` is server time.
 */
export function detailLine(
  errand: Pick<
    Errand,
    "state" | "expiresAt" | "confirmBy" | "stateChangedAt" | "reward" | "supplier" | "deliveryLocation"
  >,
  viewer: "requester" | "courier" | "admin" | "none",
  now: number,
): string | null {
  if (FINAL_STATES.includes(errand.state)) return endedText(errand);
  const deadline = deadlineText(errand, now);
  if (errand.state === "OPEN") {
    return deadline ?? (viewer === "admin" ? timeInStateText(errand, now) : null);
  }
  switch (viewer) {
    case "requester":
      if (errand.state === "ACCEPTED") return "Waiting for the courier to pick it up.";
      if (errand.state === "PICKED_UP") return `On the way to ${errand.deliveryLocation}.`;
      return confirmByText(errand, now) ?? "Delivered. Confirm it when you have it.";
    case "courier":
      if (errand.state === "ACCEPTED") {
        return `Pick it up at ${errand.supplier.name}, then mark it picked up.`;
      }
      if (errand.state === "PICKED_UP") {
        return `Take it to ${errand.deliveryLocation}, then mark it delivered.`;
      }
      return paidWhenText(errand, now);
    case "admin":
      return deadline ?? timeInStateText(errand, now);
    default:
      return deadline;
  }
}

/**
 * For the courier of a DELIVERED errand: they are paid the reward when the
 * requester confirms, or when it confirms itself at `confirmBy` (F3.9.4).
 */
function paidWhenText(errand: Pick<Errand, "reward" | "confirmBy">, now: number): string {
  const reward = creditsText(errand.reward);
  const left = countdown(errand.confirmBy, now);
  if (!left || !errand.confirmBy) return `You're paid ${reward} when the requester confirms.`;
  if (left.due) return `Confirming… You're paid ${reward} once it completes.`;
  const by = formatDateTime(errand.confirmBy);
  return `You're paid ${reward} when the requester confirms, or automatically by ${by}.`;
}

/**
 * For a requester whose errand is DELIVERED: when to confirm by, and that it
 * confirms itself after that (F3.4.2), e.g. "Confirm by 19 Oct, 17:06 ·
 * confirms itself in 23 h". Null without a `confirmBy`.
 */
export function confirmByText(errand: Pick<Errand, "confirmBy">, now: number): string | null {
  const left = countdown(errand.confirmBy, now);
  if (!left || !errand.confirmBy) return null;
  const by = `Confirm by ${formatDateTime(errand.confirmBy)}`;
  return left.due ? `${by} · confirming…` : `${by} · confirms itself in ${left.text}`;
}

/** "In this state for 3 h", for the admin list, from `stateChangedAt`. */
export function timeInStateText(errand: Pick<Errand, "stateChangedAt">, now: number): string | null {
  const age = elapsed(errand.stateChangedAt, now);
  return age === null ? null : `In this state for ${age}`;
}

/** "1 credit" or "N credits". */
export function creditsText(amount: number): string {
  return amount === 1 ? "1 credit" : `${amount} credits`;
}

/**
 * A courier's ended assignment opens its errand's detail unless it was
 * RELEASED: after withdrawing, the courier is no longer a party, and once
 * someone else accepts the errand or it ends, they may not see it.
 */
export function canOpenAssignment(record: Pick<AssignmentRecord, "outcome">): boolean {
  return record.outcome !== "RELEASED";
}

/**
 * The button for each action order-service lists. Buttons are built only from
 * an errand's `actions`, in the server's order; nothing here decides which
 * appear.
 */
export const ACTION_LABEL: Record<ErrandAction, string> = {
  amend: "Edit",
  accept: "Accept",
  withdraw: "Withdraw",
  pickup: "Mark picked up",
  deliver: "Mark delivered",
  confirm: "Confirm delivery",
  cancel: "Cancel errand",
  complete: "Mark completed",
};

/** The same button while its call is in flight. */
export const ACTION_BUSY_LABEL: Record<ErrandAction, string> = {
  amend: "Saving…",
  accept: "Accepting…",
  withdraw: "Withdrawing…",
  pickup: "Saving…",
  deliver: "Saving…",
  confirm: "Confirming…",
  cancel: "Cancelling…",
  complete: "Completing…",
};

/**
 * The confirmation step an action goes through before its call is sent, as
 * an inline box in the detail. `consequence` states what happens to credits
 * (F7.4.2). `danger` steps end or give up an errand; `neutral` ones settle it.
 */
export interface ConfirmStep {
  question: string;
  consequence: string;
  confirmLabel: string;
  tone: "danger" | "neutral";
}

/**
 * A requester's or courier's confirmation step for `action`, or null for one
 * that goes straight ahead: amend opens the edit form, and accept, pick up and
 * deliver need no step. An admin's cancel has its own, adminCancelStep.
 */
export function confirmStepFor(
  action: ErrandAction,
  errand: Pick<Errand, "reward">,
): ConfirmStep | null {
  const reward = errand.reward;
  switch (action) {
    case "withdraw":
      return {
        question: "Withdraw from this errand?",
        consequence:
          "It goes back on the open list for someone else, and you can't accept it again.",
        confirmLabel: "Yes, withdraw",
        tone: "danger",
      };
    case "confirm":
      return {
        question: "Confirm delivery?",
        consequence: `This pays the courier ${creditsText(reward)} and can't be undone.`,
        confirmLabel: "Yes, confirm",
        tone: "neutral",
      };
    case "cancel":
      return {
        question: "Cancel this errand?",
        consequence: `You get back the ${creditsText(reward)} held for it. This can't be undone.`,
        confirmLabel: "Yes, cancel errand",
        tone: "danger",
      };
    case "complete":
      return {
        question: "Mark this errand completed?",
        consequence: `The courier is paid ${creditsText(reward)}. This can't be undone.`,
        confirmLabel: "Yes, mark completed",
        tone: "neutral",
      };
    default:
      return null;
  }
}

/**
 * An admin's cancel: the requester is refunded, and an assigned courier is
 * not paid, which the step says only when there is one.
 */
export function adminCancelStep(errand: Pick<Errand, "reward" | "courierId">): ConfirmStep {
  const unpaid = errand.courierId ? " The courier is not paid." : "";
  return {
    question: "Cancel this errand?",
    consequence: `The requester gets ${creditsText(errand.reward)} back.${unpaid} This can't be undone.`,
    confirmLabel: "Yes, cancel errand",
    tone: "danger",
  };
}

/** The message after a requester's or courier's action succeeds. */
export function actionDoneText(action: ErrandAction, errand: Pick<Errand, "reward">): string {
  const reward = creditsText(errand.reward);
  switch (action) {
    case "amend":
      return "Errand updated";
    case "accept":
      return "Accepted. It's in My errands.";
    case "withdraw":
      return "Withdrawn. It's back on the open list.";
    case "pickup":
      return "Marked picked up";
    case "deliver":
      return "Marked delivered. The requester can confirm it now.";
    case "confirm":
      return `Delivery confirmed · ${reward} paid to the courier`;
    case "cancel":
      return `Errand cancelled · ${reward} returned to you`;
    case "complete":
      return `Errand completed · the courier is paid ${reward}`;
  }
}

/** The message after an admin's cancel succeeds. */
export function adminCancelDoneText(errand: Pick<Errand, "reward">): string {
  return `Errand cancelled · the requester gets ${creditsText(errand.reward)} back`;
}

const MSG_NO_RESPONSE = "Couldn't reach the server. Check your connection and try again.";
const MSG_NOT_ANSWERING = "The errand service isn't answering. Try again in a moment.";
const MSG_SIGN_IN = "Your sign-in wasn't accepted. Sign in again.";
const MSG_GONE = "This errand is no longer available.";

/**
 * Find errands' message when an errand's detail answers 404: the courier is
 * not a party, so they may see it only while it is OPEN and before its
 * deadline. The 404 does not say which ended it, so this names both.
 */
export const MSG_NOT_OPEN_ANY_MORE =
  "That errand isn't open any more. It may have expired or been taken.";
const MSG_CHECK_FORM = "Check the form and try again.";
const MSG_TOO_LONG = "That's too long to send. Shorten it and try again.";
const MSG_SOMETHING_WRONG = "Something went wrong. Try again.";

/**
 * Shown when a create got no usable answer (status 0, or the gateway's 502):
 * the errand may or may not exist. Pressing Post again is safe because the
 * retry carries the same X-Request-Id, and a key order-service has already
 * seen answers with the errand it created (F7.2).
 */
export const MSG_POST_UNCERTAIN =
  "We couldn't tell whether your errand was posted. Press Post again; it won't be posted twice.";

/**
 * order-service's 400 for a value the JSON decoder could not read starts
 * with this, followed by Go's own wording, which means nothing to a user.
 */
const DECODER_400_PREFIX = "the request body has an invalid value:";

/**
 * The text a screen shows for a failed order-service call:
 * - 0: no response; 502: the gateway could not reach order-service.
 * - 400: order-service's message, except the decoder's raw text.
 * - 401: still refused after the transport refreshed and retried once.
 * - 404: the errand is gone, or this viewer may no longer see it.
 * - 403, 409, 422, 503: order-service's message, which is written for users.
 * - 500 and anything else unexpected: a generic message.
 *
 * What the screen does next (refetch, close the detail, link to Credits) is
 * answered by needsRefetch, isErrandGone and isShortOfCredits.
 */
export function orderErrorMessage(err: unknown): string {
  if (!(err instanceof OrdersApiError)) return MSG_SOMETHING_WRONG;
  switch (err.status) {
    case 0:
      return MSG_NO_RESPONSE;
    case 502:
      return MSG_NOT_ANSWERING;
    case 400:
      return err.message.startsWith(DECODER_400_PREFIX) ? MSG_CHECK_FORM : serverText(err, MSG_CHECK_FORM);
    case 401:
      return MSG_SIGN_IN;
    case 404:
      return MSG_GONE;
    case 413:
      return MSG_TOO_LONG;
    case 403:
    case 409:
    case 422:
      return serverText(err, MSG_SOMETHING_WRONG);
    case 503:
      return serverText(err, MSG_NOT_ANSWERING);
    default:
      return MSG_SOMETHING_WRONG;
  }
}

/**
 * The text for a failed create. Same as orderErrorMessage, except that no
 * usable answer (0 or 502) is MSG_POST_UNCERTAIN: the form keeps its values
 * and its request key, so pressing Post again cannot post twice.
 */
export function createErrorMessage(err: unknown): string {
  if (err instanceof OrdersApiError && (err.status === 0 || err.status === 502)) {
    return MSG_POST_UNCERTAIN;
  }
  return orderErrorMessage(err);
}

/**
 * 404: the errand no longer exists for this viewer. Close its detail and
 * refetch the list.
 */
export function isErrandGone(err: unknown): boolean {
  return err instanceof OrdersApiError && err.status === 404;
}

/**
 * 403, 404 or 409: the screen's copy of the errand is out of date, so it
 * refetches before anything else is tried. A 409 refetch that answers 404 is
 * then handled as gone.
 */
export function needsRefetch(err: unknown): boolean {
  return err instanceof OrdersApiError && [403, 404, 409].includes(err.status);
}

/** 422: the reward is more than the requester's available credits. Nothing changed. */
export function isShortOfCredits(err: unknown): boolean {
  return err instanceof OrdersApiError && err.status === 422;
}

/**
 * order-service's own message as a sentence, or `fallback` when the body had
 * none (the transport's "Request failed (N)").
 */
function serverText(err: OrdersApiError, fallback: string): string {
  return err.message.startsWith("Request failed (") ? fallback : sentence(err.message, ".");
}

/** Capitalises the first letter and ends with `stop` unless punctuation is already there. */
function sentence(text: string, stop: string): string {
  const trimmed = text.trim();
  if (!trimmed) return trimmed;
  const capitalised = trimmed[0]!.toUpperCase() + trimmed.slice(1);
  return /[.!?…]$/.test(capitalised) ? capitalised : capitalised + stop;
}
