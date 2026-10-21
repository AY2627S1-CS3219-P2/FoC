// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-18
// Scope: Tests for the errand detail's logic: how the viewer stands to an
//   errand, which people it names to each viewer, and which buttons it draws
//   from the server's actions.
// Author review: James Teo — reviewed and verified.

import { describe, expect, it } from "vitest";
import { ACTION_BUTTON_CLASS, buttonsFor, partiesShown, relationshipOf } from "./ErrandDetail";
import { ERRAND_ACTIONS, type ErrandAction } from "./orderTypes";

const REQUESTER = "11111111-1111-4111-8111-111111111111";
const COURIER = "22222222-2222-4222-8222-222222222222";
const SOMEONE = "33333333-3333-4333-8333-333333333333";

describe("relationshipOf", () => {
  const errand = { requesterId: REQUESTER, courierId: COURIER };

  it("knows the requester and the courier by the errand's ids", () => {
    expect(relationshipOf(errand, REQUESTER, "STUDENT")).toBe("requester");
    expect(relationshipOf(errand, COURIER, "STUDENT")).toBe("courier");
  });

  it("treats any admin as admin", () => {
    expect(relationshipOf(errand, SOMEONE, "ADMIN")).toBe("admin");
  });

  it("is none for a student browsing an open errand, whose ids are not sent", () => {
    expect(relationshipOf({}, SOMEONE, "STUDENT")).toBe("none");
    expect(relationshipOf({ requesterId: REQUESTER }, SOMEONE, "STUDENT")).toBe("none");
  });
});

describe("partiesShown", () => {
  it("shows the requester no one until a courier is assigned, then the courier", () => {
    expect(partiesShown({ requesterId: REQUESTER }, "requester")).toEqual([]);
    expect(partiesShown({ requesterId: REQUESTER, courierId: COURIER }, "requester")).toEqual([
      { role: "Courier", uid: COURIER },
    ]);
  });

  it("shows the courier the requester", () => {
    expect(partiesShown({ requesterId: REQUESTER, courierId: COURIER }, "courier")).toEqual([
      { role: "Requester", uid: REQUESTER },
    ]);
  });

  it("shows an admin both people, requester first", () => {
    expect(partiesShown({ requesterId: REQUESTER, courierId: COURIER }, "admin")).toEqual([
      { role: "Requester", uid: REQUESTER },
      { role: "Courier", uid: COURIER },
    ]);
    expect(partiesShown({ requesterId: REQUESTER }, "admin")).toEqual([
      { role: "Requester", uid: REQUESTER },
    ]);
  });

  it("shows anyone else nobody", () => {
    expect(partiesShown({ requesterId: REQUESTER, courierId: COURIER }, "none")).toEqual([]);
  });
});

describe("buttonsFor", () => {
  it("draws exactly the server's actions, in the server's order", () => {
    expect(buttonsFor({ actions: ["amend", "cancel"] })).toEqual(["amend", "cancel"]);
    expect(buttonsFor({ actions: ["withdraw", "pickup"] })).toEqual(["withdraw", "pickup"]);
    expect(buttonsFor({ actions: [] })).toEqual([]);
  });

  it("draws no button for an action this build does not know", () => {
    const actions = ["confirm", "dispute"] as unknown as ErrandAction[];
    expect(buttonsFor({ actions })).toEqual(["confirm"]);
  });

  it("styles every action, cancel as the dangerous one", () => {
    for (const action of ERRAND_ACTIONS) {
      expect(ACTION_BUTTON_CLASS[action]).toMatch(/^btn-/);
    }
    expect(ACTION_BUTTON_CLASS.cancel).toBe("btn-danger");
  });
});
