// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-18
// Scope: Tests for the errand form's logic: the client-side checks, the
//   create body, the amend changes (only what differs, "" clearing the note),
//   the form filled from an errand, whether the form has unsaved changes, the
//   edits kept when the errand changed under them, and the balance hint.
// Author review: James Teo — reviewed and verified.

import { describe, expect, it } from "vitest";
import {
  balanceHint,
  emptyErrandForm,
  errandChanges,
  formChanged,
  formValuesFrom,
  keepEdits,
  parseReward,
  textLength,
  toCreateRequest,
  validateErrandForm,
  type ErrandFormValues,
} from "./ErrandForm";
import type { Errand } from "./orderTypes";

const LOCATIONS = ["COM3 Lobby", "The Deck, AS8"];

function filled(overrides: Partial<ErrandFormValues> = {}): ErrandFormValues {
  return {
    supplier: { id: "s-1", name: "Spinelli Coffee" },
    itemDescription: "1 × Iced latte",
    deliveryLocation: "COM3 Lobby",
    deliveryNote: "",
    reward: "10",
    ...overrides,
  };
}

const ERRAND: Errand = {
  id: "e-1",
  state: "OPEN",
  version: 3,
  supplier: { id: "s-1", name: "Spinelli Coffee", location: "COM1" },
  itemDescription: "1 × Iced latte",
  deliveryLocation: "COM3 Lobby",
  deliveryNote: "Grey backpack",
  reward: 10,
  requesterId: "r-1",
  createdAt: "2026-10-18T09:00:00Z",
  updatedAt: "2026-10-18T09:00:00Z",
  stateChangedAt: "2026-10-18T09:00:00Z",
  expiresAt: "2026-10-18T11:00:00Z",
  actions: ["amend", "cancel"],
};

describe("textLength", () => {
  it("counts code points, as order-service does, so an emoji is one", () => {
    expect(textLength("☕")).toBe(1);
    expect(textLength("🧋🧋")).toBe(2);
    expect("🧋🧋".length).toBe(4);
  });
});

describe("parseReward", () => {
  it.each([
    ["10", 10],
    [" 7 ", 7],
    ["0", 0],
    ["", null],
    ["1.5", null],
    ["-3", null],
    ["ten", null],
    ["1e3", null],
  ])("reads %j as %j", (text, reward) => {
    expect(parseReward(text)).toBe(reward);
  });
});

describe("validateErrandForm", () => {
  it("passes a filled form", () => {
    expect(validateErrandForm(filled(), LOCATIONS)).toEqual({});
  });

  it("flags everything a blank form is missing, but not the optional note", () => {
    const problems = validateErrandForm({ ...emptyErrandForm(), reward: "" }, LOCATIONS);
    expect(Object.keys(problems).sort()).toEqual(
      ["deliveryLocation", "itemDescription", "reward", "supplier"].sort(),
    );
  });

  it("treats an item of only spaces as empty", () => {
    expect(validateErrandForm(filled({ itemDescription: "   " }), LOCATIONS).itemDescription).toBe(
      "Say what to get.",
    );
  });

  it("allows 280 characters of item, counted as code points, and no more", () => {
    expect(validateErrandForm(filled({ itemDescription: "🧋".repeat(280) }), LOCATIONS)).toEqual({});
    expect(
      validateErrandForm(filled({ itemDescription: "a".repeat(281) }), LOCATIONS).itemDescription,
    ).toBe("Keep it to 280 characters.");
  });

  it("allows 140 characters of note and no more", () => {
    expect(validateErrandForm(filled({ deliveryNote: "n".repeat(140) }), LOCATIONS)).toEqual({});
    expect(validateErrandForm(filled({ deliveryNote: "n".repeat(141) }), LOCATIONS).deliveryNote).toBe(
      "Keep the note to 140 characters.",
    );
  });

  it("wants a location from the list order-service gave", () => {
    expect(
      validateErrandForm(filled({ deliveryLocation: "My room" }), LOCATIONS).deliveryLocation,
    ).toBe("Choose a location from the list.");
  });

  it.each(["0", "-1", "2.5", "", "abc"])("refuses a reward of %j", (reward) => {
    expect(validateErrandForm(filled({ reward }), LOCATIONS).reward).toBeDefined();
  });

  it("leaves affordability to the server: a huge reward passes the form", () => {
    expect(validateErrandForm(filled({ reward: "500" }), LOCATIONS)).toEqual({});
  });
});

describe("toCreateRequest", () => {
  it("sends the contract's fields, trimmed", () => {
    expect(
      toCreateRequest(filled({ itemDescription: "  Latte  ", deliveryNote: "  Grey backpack " })),
    ).toEqual({
      supplierId: "s-1",
      itemDescription: "Latte",
      deliveryLocation: "COM3 Lobby",
      deliveryNote: "Grey backpack",
      reward: 10,
    });
  });

  it("leaves a blank note out", () => {
    expect(toCreateRequest(filled({ deliveryNote: "   " })).deliveryNote).toBeUndefined();
  });

  it("keeps the note's line breaks", () => {
    expect(toCreateRequest(filled({ deliveryNote: "Level 2\nTable 4" })).deliveryNote).toBe(
      "Level 2\nTable 4",
    );
  });
});

describe("formValuesFrom", () => {
  it("fills the form from an errand", () => {
    expect(formValuesFrom(ERRAND)).toEqual({
      supplier: { id: "s-1", name: "Spinelli Coffee" },
      itemDescription: "1 × Iced latte",
      deliveryLocation: "COM3 Lobby",
      deliveryNote: "Grey backpack",
      reward: "10",
    });
  });

  it("gives an empty note for an errand without one", () => {
    expect(formValuesFrom({ ...ERRAND, deliveryNote: undefined }).deliveryNote).toBe("");
  });
});

describe("errandChanges", () => {
  it("is empty when nothing changed", () => {
    expect(errandChanges(ERRAND, formValuesFrom(ERRAND))).toEqual({});
  });

  it("sends only the fields that differ", () => {
    const values = { ...formValuesFrom(ERRAND), reward: "15", deliveryLocation: "The Deck, AS8" };
    expect(errandChanges(ERRAND, values)).toEqual({ reward: 15, deliveryLocation: "The Deck, AS8" });
  });

  it("sends the new supplier's id", () => {
    const values = { ...formValuesFrom(ERRAND), supplier: { id: "s-2", name: "Starbucks" } };
    expect(errandChanges(ERRAND, values)).toEqual({ supplierId: "s-2" });
  });

  it("sends a trimmed item, and nothing when only spaces were added", () => {
    expect(errandChanges(ERRAND, { ...formValuesFrom(ERRAND), itemDescription: " Mocha " })).toEqual({
      itemDescription: "Mocha",
    });
    expect(
      errandChanges(ERRAND, { ...formValuesFrom(ERRAND), itemDescription: "1 × Iced latte  " }),
    ).toEqual({});
  });

  it("clears the note with an empty string", () => {
    expect(errandChanges(ERRAND, { ...formValuesFrom(ERRAND), deliveryNote: "  " })).toEqual({
      deliveryNote: "",
    });
  });

  it("does not send a note for an errand that had none and still has none", () => {
    const noNote = { ...ERRAND, deliveryNote: undefined };
    expect(errandChanges(noNote, formValuesFrom(noNote))).toEqual({});
  });

  it("adds a note to an errand that had none", () => {
    const noNote = { ...ERRAND, deliveryNote: undefined };
    expect(errandChanges(noNote, { ...formValuesFrom(noNote), deliveryNote: "By the lift" })).toEqual({
      deliveryNote: "By the lift",
    });
  });
});

describe("formChanged", () => {
  it("is false for the values the form opened with", () => {
    expect(formChanged(filled(), filled())).toBe(false);
    expect(formChanged(emptyErrandForm(), emptyErrandForm())).toBe(false);
  });

  it.each([
    ["the supplier", { supplier: { id: "s-2", name: "Fong Seng" } }],
    ["the supplier, cleared", { supplier: null }],
    ["the item", { itemDescription: "2 × Iced latte" }],
    ["the location", { deliveryLocation: "The Deck, AS8" }],
    ["the note", { deliveryNote: "By the door" }],
    ["the reward", { reward: "12" }],
    ["the reward, half-typed", { reward: "" }],
  ] as const)("is true once %s changes", (_, change) => {
    expect(formChanged(filled(), filled(change))).toBe(true);
  });

  it("goes back to false when a change is undone", () => {
    expect(formChanged(filled(), filled({ reward: "10" }))).toBe(false);
  });
});

describe("keepEdits", () => {
  // The errand as the edit opened, the student's boxes, and the errand after
  // someone else saved a change (another tab raised the reward to 9).
  const before = filled({ deliveryNote: "", reward: "10" });
  const latest = filled({ deliveryNote: "", reward: "9" });

  it("keeps what the student changed and takes the rest from the latest errand", () => {
    const typed = filled({ deliveryNote: "By the lift lobby", reward: "8" });
    expect(keepEdits(before, typed, latest)).toEqual(
      filled({ deliveryNote: "By the lift lobby", reward: "8" }),
    );
  });

  it("takes the latest value of a field the student left alone", () => {
    const typed = filled({ deliveryNote: "By the lift lobby", reward: "10" });
    expect(keepEdits(before, typed, latest)).toEqual(
      filled({ deliveryNote: "By the lift lobby", reward: "9" }),
    );
  });

  it("keeps a changed supplier, compared by id", () => {
    const typed = filled({ supplier: { id: "s-2", name: "Starbucks" } });
    const moved = filled({ supplier: { id: "s-1", name: "Spinelli Coffee (renamed)" } });
    expect(keepEdits(before, typed, moved).supplier).toEqual({ id: "s-2", name: "Starbucks" });
    expect(keepEdits(before, before, moved).supplier).toEqual(moved.supplier);
  });

  it("is the latest errand when nothing was changed", () => {
    expect(keepEdits(before, before, latest)).toEqual(latest);
    expect(formChanged(latest, keepEdits(before, before, latest))).toBe(false);
  });
});

describe("balanceHint", () => {
  it("says what a new errand leaves available", () => {
    expect(balanceHint(100, 10, 0)).toBe("Leaves you 90 credits available.");
    expect(balanceHint(11, 10, 0)).toBe("Leaves you 1 credit available.");
  });

  it("counts an edit against what the errand already holds", () => {
    // 85 available with 15 held: lowering the reward to 8 frees 7.
    expect(balanceHint(85, 8, 15)).toBe("Leaves you 92 credits available.");
    // Raising 10 to 15 with 3 available is 2 short.
    expect(balanceHint(3, 15, 10)).toBe("You're 2 credits short of this reward.");
  });

  it("is left out when the balance or the reward is unknown", () => {
    expect(balanceHint(null, 10, 0)).toBeNull();
    expect(balanceHint(100, null, 0)).toBeNull();
  });
});
