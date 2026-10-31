// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-18
// Scope: Tests for the errand display text — a label for every state,
//   history action and outcome, the amended-field lines, the deadline,
//   progress, ended and time-in-state lines, the detail's line for each
//   viewer in each state, the action buttons, confirmation steps
//   and success messages, and the message and follow-up for each failed
//   order-service call, including the lost-response case on create and the
//   409s for a sixth open errand and for re-accepting after a withdrawal.
// Author review: James Teo — reviewed and verified.

import { describe, expect, it } from "vitest";
import {
  ACTION_BUSY_LABEL,
  ACTION_LABEL,
  actionDoneText,
  adminCancelDoneText,
  amendDoneText,
  adminCancelStep,
  canOpenAssignment,
  confirmByText,
  confirmStepFor,
  createErrorMessage,
  creditsText,
  deadlineText,
  describeChange,
  detailLine,
  endedText,
  historyLabel,
  isErrandGone,
  isShortOfCredits,
  MSG_NOT_OPEN_ANY_MORE,
  MSG_POST_UNCERTAIN,
  needsRefetch,
  OUTCOME_LABEL,
  orderErrorMessage,
  progressText,
  STATE_LABEL,
  timeInStateText,
} from "./format";
import { OrdersApiError } from "./ordersApi";
import { ERRAND_ACTIONS, ERRAND_STATUSES, HISTORY_ACTIONS, type ErrandStatus } from "./orderTypes";

const SERVER = Date.parse("2026-10-18T09:00:00Z");
const MINUTE = 60_000;

function at(ms: number): string {
  return new Date(SERVER + ms).toISOString();
}

function error(status: number, message = `Request failed (${status})`): OrdersApiError {
  return new OrdersApiError(message, status);
}

describe("labels", () => {
  it("has a label for every errand state", () => {
    for (const state of ERRAND_STATUSES) {
      expect(STATE_LABEL[state]).toMatch(/^[A-Z][a-z ]+$/);
    }
  });

  it("has a distinct label for every history action, not its code", () => {
    const labels = HISTORY_ACTIONS.map(historyLabel);
    for (const [i, label] of labels.entries()) {
      expect(label).not.toBe("");
      expect(label).not.toBe(HISTORY_ACTIONS[i]);
    }
    expect(new Set(labels).size).toBe(HISTORY_ACTIONS.length);
  });

  it("tells an admin's cancel and complete apart from the parties' own", () => {
    expect(historyLabel("CANCEL")).toBe("Cancelled");
    expect(historyLabel("ADMIN_CANCEL")).toBe("Cancelled by an admin");
    expect(historyLabel("AUTO_CONFIRM")).toBe("Confirmed automatically");
  });

  it("shows an action this build does not know in words", () => {
    expect(historyLabel("ADMIN_REOPEN")).toBe("Admin reopen");
  });

  it("names a withdrawn assignment Released", () => {
    expect(OUTCOME_LABEL.RELEASED).toBe("Released");
  });
});

describe("describeChange", () => {
  it.each([
    [{ field: "reward", from: "10", to: "15" }, "Reward: 10 → 15"],
    [{ field: "supplier", from: "Spinelli Coffee", to: "Starbucks" }, "Supplier: Spinelli Coffee → Starbucks"],
    [{ field: "itemDescription", from: "Latte", to: "Mocha" }, "Item: Latte → Mocha"],
    [{ field: "deliveryLocation", from: "COM3 Lobby", to: "The Deck, AS8" }, "Delivery location: COM3 Lobby → The Deck, AS8"],
    [{ field: "deliveryNote", from: "", to: "Grey backpack" }, "Note: none → Grey backpack"],
    [{ field: "deliveryNote", from: "Grey backpack", to: "" }, "Note: Grey backpack → none"],
    // What the assigned courier is sent: the note changed, but not to what.
    [{ field: "deliveryNote", from: "", to: "" }, "Note changed"],
  ] as const)("writes %o as %s", (change, text) => {
    expect(describeChange(change)).toBe(text);
  });
});

describe("deadlineText", () => {
  it("counts down to expiry while the errand has an expiresAt", () => {
    expect(deadlineText({ expiresAt: at(112 * MINUTE) }, SERVER)).toBe("Expires in 1 h 52 min");
  });

  it("says Expiring… once the expiry is reached", () => {
    expect(deadlineText({ expiresAt: at(0) }, SERVER)).toBe("Expiring…");
    expect(deadlineText({ expiresAt: at(-4_000) }, SERVER)).toBe("Expiring…");
  });

  it("counts down to the automatic confirmation while it has a confirmBy", () => {
    expect(deadlineText({ confirmBy: at(23 * 60 * MINUTE) }, SERVER)).toBe("Confirms itself in 23 h");
  });

  it("says Confirming… once the confirmation time is reached", () => {
    expect(deadlineText({ confirmBy: at(-1) }, SERVER)).toBe("Confirming…");
  });

  it("is null for an errand with neither deadline", () => {
    expect(deadlineText({}, SERVER)).toBeNull();
  });
});

describe("timeInStateText", () => {
  it("says how long the errand has been in its state", () => {
    expect(timeInStateText({ stateChangedAt: at(-3 * 60 * MINUTE) }, SERVER)).toBe(
      "In this state for 3 h",
    );
  });

  it("is null when the time cannot be read", () => {
    expect(timeInStateText({ stateChangedAt: "" }, SERVER)).toBeNull();
  });
});

describe("orderErrorMessage", () => {
  it.each([
    [0, "No usable response from the server", "Couldn't reach the server. Check your connection and try again."],
    [502, "upstream service unavailable", "The errand service isn't answering. Try again in a moment."],
    [
      400,
      "the request body has an invalid value: invalid UUID length: 8",
      "Check the form and try again.",
    ],
    [400, "the reward must be at least 1 credit", "The reward must be at least 1 credit."],
    [401, "missing, invalid or expired access token", "Your sign-in wasn't accepted. Sign in again."],
    [403, "only the errand's requester can do this", "Only the errand's requester can do this."],
    [404, "errand not found", "This errand is no longer available."],
    [409, "someone else accepted this errand first", "Someone else accepted this errand first."],
    [413, "request body too large", "That's too long to send. Shorten it and try again."],
    [422, "not enough available credits for this reward", "Not enough available credits for this reward."],
    [500, "internal error", "Something went wrong. Try again."],
    [
      503,
      "credits are unavailable right now; please try again",
      "Credits are unavailable right now; please try again.",
    ],
  ])("shows a %i as the screen should", (status, message, text) => {
    expect(orderErrorMessage(error(status, message))).toBe(text);
  });

  it("keeps a message that already ends a sentence", () => {
    expect(orderErrorMessage(error(409, "too late!"))).toBe("Too late!");
  });

  it("falls back when a refusal came without order-service's message", () => {
    expect(orderErrorMessage(error(409))).toBe("Something went wrong. Try again.");
    expect(orderErrorMessage(error(503))).toBe(
      "The errand service isn't answering. Try again in a moment.",
    );
    expect(orderErrorMessage(error(400))).toBe("Check the form and try again.");
  });

  it("is generic for anything that is not an OrdersApiError", () => {
    expect(orderErrorMessage(new TypeError("x is undefined"))).toBe("Something went wrong. Try again.");
  });
});

describe("createErrorMessage", () => {
  it.each([0, 502])("says a create with no usable answer (%i) is safe to post again", (status) => {
    expect(createErrorMessage(error(status))).toBe(MSG_POST_UNCERTAIN);
  });

  it("shows other failures as orderErrorMessage does", () => {
    const short = error(422, "not enough available credits for this reward");
    expect(createErrorMessage(short)).toBe(orderErrorMessage(short));
    const busy = error(503, "credits are unavailable right now; please try again");
    expect(createErrorMessage(busy)).toBe("Credits are unavailable right now; please try again.");
  });
});

describe("an errand gone from Find errands", () => {
  it("claims no single reason, since the 404 gives none", () => {
    expect(MSG_NOT_OPEN_ANY_MORE).toBe(
      "That errand isn't open any more. It may have expired or been taken.",
    );
  });
});

describe("what a screen does after a failure", () => {
  it("closes the detail only for a 404", () => {
    expect(isErrandGone(error(404))).toBe(true);
    expect(isErrandGone(error(409))).toBe(false);
    expect(isErrandGone(new Error("404"))).toBe(false);
  });

  it.each([
    [403, true],
    [404, true],
    [409, true],
    [400, false],
    [422, false],
    [500, false],
    [503, false],
    [0, false],
  ])("refetches after a %i: %s", (status, refetch) => {
    expect(needsRefetch(error(status))).toBe(refetch);
  });

  it("links to Credits only for a 422", () => {
    expect(isShortOfCredits(error(422))).toBe(true);
    expect(isShortOfCredits(error(409))).toBe(false);
  });
});

describe("progressText", () => {
  const base = { stateChangedAt: at(-5 * MINUTE) };

  it("is the countdown while the errand has a deadline", () => {
    expect(progressText({ ...base, state: "OPEN", expiresAt: at(4 * MINUTE) }, SERVER)).toBe(
      "Expires in 4 min",
    );
    expect(
      progressText({ ...base, state: "DELIVERED", confirmBy: at(23 * 60 * MINUTE) }, SERVER),
    ).toBe("Confirms itself in 23 h");
  });

  it("says what an errand without a running deadline waits for", () => {
    expect(progressText({ ...base, state: "ACCEPTED" }, SERVER)).toBe("Waiting to be picked up");
    expect(progressText({ ...base, state: "PICKED_UP" }, SERVER)).toBe("On the way");
  });

  it("says when a final errand ended, by its state's label", () => {
    for (const state of ["COMPLETED", "CANCELLED", "EXPIRED"] as const) {
      expect(progressText({ ...base, state }, SERVER)).toMatch(new RegExp(`^${STATE_LABEL[state]} `));
    }
  });

  it("falls back to the label alone when the time cannot be read", () => {
    expect(progressText({ state: "EXPIRED", stateChangedAt: "" }, SERVER)).toBe("Expired");
  });
});

describe("endedText", () => {
  it("is the state's label and when the errand reached it", () => {
    expect(endedText({ state: "CANCELLED", stateChangedAt: at(0) })).toMatch(/^Cancelled \d+ Oct, \d\d:\d\d$/);
  });

  it("is the label alone when the time cannot be read", () => {
    expect(endedText({ state: "COMPLETED", stateChangedAt: "" })).toBe("Completed");
  });
});

describe("detailLine", () => {
  const errand = {
    stateChangedAt: at(-5 * MINUTE),
    reward: 10,
    supplier: { id: "s-1", name: "The Coffee Roaster", location: "Blk AS8, level 1" },
    deliveryLocation: "COM3 Lobby",
  };
  const inState = (state: ErrandStatus, deadlines: { expiresAt?: string; confirmBy?: string } = {}) => ({
    ...errand,
    ...deadlines,
    state,
  });

  it("counts down an OPEN errand for every viewer", () => {
    const open = inState("OPEN", { expiresAt: at(112 * MINUTE) });
    for (const viewer of ["requester", "courier", "admin", "none"] as const) {
      expect(detailLine(open, viewer, SERVER)).toBe("Expires in 1 h 52 min");
    }
  });

  it("tells the requester who has the errand and where it is going", () => {
    expect(detailLine(inState("ACCEPTED"), "requester", SERVER)).toBe(
      "Waiting for the courier to pick it up.",
    );
    expect(detailLine(inState("PICKED_UP"), "requester", SERVER)).toBe("On the way to COM3 Lobby.");
  });

  it("tells the requester of a delivered errand when to confirm by", () => {
    const delivered = inState("DELIVERED", { confirmBy: at(23 * 60 * MINUTE) });
    expect(detailLine(delivered, "requester", SERVER)).toMatch(
      /^Confirm by .+ · confirms itself in 23 h$/,
    );
    expect(detailLine(inState("DELIVERED"), "requester", SERVER)).toBe(
      "Delivered. Confirm it when you have it.",
    );
  });

  it("tells the courier the next step", () => {
    expect(detailLine(inState("ACCEPTED"), "courier", SERVER)).toBe(
      "Pick it up at The Coffee Roaster, then mark it picked up.",
    );
    expect(detailLine(inState("PICKED_UP"), "courier", SERVER)).toBe(
      "Take it to COM3 Lobby, then mark it delivered.",
    );
  });

  it("tells the courier of a delivered errand when they are paid", () => {
    const delivered = inState("DELIVERED", { confirmBy: at(23 * 60 * MINUTE) });
    expect(detailLine(delivered, "courier", SERVER)).toMatch(
      /^You're paid 10 credits when the requester confirms, or automatically by \d+ Oct, \d\d:\d\d\.$/,
    );
    expect(detailLine({ ...delivered, confirmBy: at(-1_000) }, "courier", SERVER)).toBe(
      "Confirming… You're paid 10 credits once it completes.",
    );
    expect(detailLine(inState("DELIVERED"), "courier", SERVER)).toBe(
      "You're paid 10 credits when the requester confirms.",
    );
  });

  it("tells an admin how long an errand has been in its state, or its countdown", () => {
    expect(detailLine(inState("ACCEPTED"), "admin", SERVER)).toBe("In this state for 5 min");
    expect(
      detailLine(inState("DELIVERED", { confirmBy: at(23 * 60 * MINUTE) }), "admin", SERVER),
    ).toBe("Confirms itself in 23 h");
  });

  it("says when a final errand ended, to every viewer, never a time in state", () => {
    for (const state of ["COMPLETED", "CANCELLED", "EXPIRED"] as const) {
      for (const viewer of ["requester", "courier", "admin"] as const) {
        const line = detailLine(inState(state), viewer, SERVER);
        expect(line).toBe(endedText(inState(state)));
        expect(line).not.toMatch(/In this state/);
      }
    }
  });

  it("has a line for every state a party can see", () => {
    for (const state of ERRAND_STATUSES) {
      const deadlines = state === "OPEN" ? { expiresAt: at(MINUTE) } : {};
      for (const viewer of ["requester", "courier", "admin"] as const) {
        expect(detailLine(inState(state, deadlines), viewer, SERVER)).toBeTruthy();
      }
    }
  });
});

describe("confirmByText", () => {
  it("names the time and the automatic confirmation", () => {
    const text = confirmByText({ confirmBy: at(23 * 60 * MINUTE) }, SERVER);
    expect(text).toMatch(/^Confirm by .+ · confirms itself in 23 h$/);
  });

  it("says confirming once the time has passed, never a negative time", () => {
    expect(confirmByText({ confirmBy: at(-2_000) }, SERVER)).toMatch(/ · confirming…$/);
  });

  it("is null without a confirmBy", () => {
    expect(confirmByText({}, SERVER)).toBeNull();
  });
});

describe("creditsText", () => {
  it("is singular for one credit only", () => {
    expect(creditsText(1)).toBe("1 credit");
    expect(creditsText(0)).toBe("0 credits");
    expect(creditsText(15)).toBe("15 credits");
  });
});

describe("canOpenAssignment", () => {
  it("opens completed and cancelled assignments, never a released one", () => {
    expect(canOpenAssignment({ outcome: "COMPLETED" })).toBe(true);
    expect(canOpenAssignment({ outcome: "CANCELLED" })).toBe(true);
    expect(canOpenAssignment({ outcome: "RELEASED" })).toBe(false);
  });
});

describe("action buttons", () => {
  it("has a button and an in-flight label for every action order-service lists", () => {
    for (const action of ERRAND_ACTIONS) {
      expect(ACTION_LABEL[action]).toMatch(/^[A-Z]/);
      expect(ACTION_BUSY_LABEL[action]).toMatch(/…$/);
    }
    expect(new Set(ERRAND_ACTIONS.map((a) => ACTION_LABEL[a])).size).toBe(ERRAND_ACTIONS.length);
  });

  it("has a success message for every action", () => {
    for (const action of ERRAND_ACTIONS) {
      expect(actionDoneText(action, { reward: 8 })).not.toBe("");
    }
  });

  it("puts the credits that move in the messages that move them", () => {
    expect(actionDoneText("confirm", { reward: 8 })).toBe(
      "Delivery confirmed · 8 credits paid to the courier",
    );
    expect(actionDoneText("cancel", { reward: 10 })).toBe("Errand cancelled · 10 credits returned to you");
    expect(actionDoneText("complete", { reward: 1 })).toBe(
      "Errand completed · the courier is paid 1 credit",
    );
    expect(adminCancelDoneText({ reward: 10 })).toBe(
      "Errand cancelled · the requester gets 10 credits back",
    );
  });

  it("says a lowered reward's credits are on the way back, and nothing else for other edits", () => {
    expect(amendDoneText({ reward: 50 }, { reward: 20 })).toBe(
      "Reward updated. The difference returns to your balance shortly.",
    );
    expect(amendDoneText({ reward: 20 }, { reward: 40 })).toBe("Errand updated");
    expect(amendDoneText({ reward: 20 }, { reward: 20 })).toBe("Errand updated");
  });
});

describe("confirmation steps", () => {
  it("asks before withdraw, confirm, cancel and complete, and not before the rest", () => {
    const stepped = ERRAND_ACTIONS.filter((action) => confirmStepFor(action, { reward: 5 }) !== null);
    expect(stepped).toEqual(["withdraw", "confirm", "cancel", "complete"]);
  });

  it("says a withdrawal can't be taken back by accepting again", () => {
    expect(confirmStepFor("withdraw", { reward: 5 })?.consequence).toBe(
      "It goes back on the open list for someone else, and you can't accept it again.",
    );
  });

  it("states the credits each step moves (F7.4.2)", () => {
    expect(confirmStepFor("confirm", { reward: 8 })?.consequence).toBe(
      "This pays the courier 8 credits and can't be undone.",
    );
    expect(confirmStepFor("cancel", { reward: 10 })?.consequence).toBe(
      "You get back the 10 credits held for it. This can't be undone.",
    );
    expect(confirmStepFor("complete", { reward: 1 })?.consequence).toBe(
      "The courier is paid 1 credit. This can't be undone.",
    );
  });

  it("uses danger colours for steps that end or give up an errand", () => {
    expect(confirmStepFor("withdraw", { reward: 5 })?.tone).toBe("danger");
    expect(confirmStepFor("cancel", { reward: 5 })?.tone).toBe("danger");
    expect(confirmStepFor("confirm", { reward: 5 })?.tone).toBe("neutral");
    expect(confirmStepFor("complete", { reward: 5 })?.tone).toBe("neutral");
  });

  it("names the unpaid courier in an admin's cancel only when one is assigned", () => {
    expect(adminCancelStep({ reward: 10 }).consequence).toBe(
      "The requester gets 10 credits back. This can't be undone.",
    );
    expect(adminCancelStep({ reward: 10, courierId: "c-1" }).consequence).toBe(
      "The requester gets 10 credits back. The courier is not paid. This can't be undone.",
    );
  });
});

describe("the newer 409s", () => {
  it("shows order-service's refusal of a sixth open errand on the create form", () => {
    const sixth = error(
      409,
      "you already have 5 open errands; cancel one or wait for one to be accepted before posting another",
    );
    expect(createErrorMessage(sixth)).toBe(
      "You already have 5 open errands; cancel one or wait for one to be accepted before posting another.",
    );
    // Nothing was saved, so the form keeps its values and its request key,
    // and nothing needs refetching.
    expect(isShortOfCredits(sixth)).toBe(false);
  });

  it("shows the refusal of an accept after withdrawing, and refetches", () => {
    const again = error(409, "you withdrew from this errand, so you cannot accept it again");
    expect(orderErrorMessage(again)).toBe(
      "You withdrew from this errand, so you cannot accept it again.",
    );
    expect(needsRefetch(again)).toBe(true);
  });
});
