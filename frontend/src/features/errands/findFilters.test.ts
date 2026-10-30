// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: Tests for Find errands' filters (D-092) and the once-per-visit
//   shown log.
// Author review: PENDING — <reviewer to complete>

import { describe, expect, it } from "vitest";
import {
  destinationGroups,
  filterErrands,
  isFiltered,
  NO_FILTERS,
  pickupOptions,
  type FindFilters,
} from "./findFilters";
import type { CampusLocation, Errand } from "./orderTypes";
import { ShownLog } from "./shownTracking";

const NOW = Date.parse("2026-10-29T10:00:00Z");
const MIN = 60_000;

function errand(id: string, over: Partial<Errand> = {}): Errand {
  return {
    id,
    state: "OPEN",
    version: 1,
    supplier: { id: "s-coffee", name: "The Coffee Roaster", location: "AS8" },
    itemDescription: `item ${id}`,
    deliveryLocation: "COM1 Lobby",
    reward: 5,
    createdAt: "2026-10-29T09:00:00Z",
    updatedAt: "2026-10-29T09:00:00Z",
    stateChangedAt: "2026-10-29T09:00:00Z",
    expiresAt: new Date(NOW + 45 * MIN).toISOString(),
    actions: ["accept"],
    ...over,
  };
}

const LOCATIONS: CampusLocation[] = [
  { name: "COM1 Lobby", area: "Business & Computing" },
  { name: "COM3 Lobby", area: "Business & Computing" },
  { name: "RC4 lobby", area: "UTown residences" },
];

const filter = (errands: Errand[], f: Partial<FindFilters>) =>
  filterErrands(errands, { ...NO_FILTERS, ...f }, NOW, LOCATIONS).map((e) => e.id);

describe("filterErrands", () => {
  const soon = errand("soon", { expiresAt: new Date(NOW + 20 * MIN).toISOString() });
  const mid = errand("mid", { expiresAt: new Date(NOW + 45 * MIN).toISOString() });
  const late = errand("late", { expiresAt: new Date(NOW + 3 * 60 * MIN).toISOString() });
  const edge = errand("edge", { expiresAt: new Date(NOW + 60 * MIN).toISOString() });
  const none = errand("none", { expiresAt: undefined });
  const all = [soon, mid, late, edge, none];

  it("keeps every errand, in order, with no filters", () => {
    expect(filter(all, {})).toEqual(["soon", "mid", "late", "edge", "none"]);
  });

  it.each([
    ["under30m", ["soon"]],
    ["under1h", ["soon", "mid", "edge"]],
    ["over1h", ["late"]],
  ] as const)("time left %s keeps %j and drops an errand with no deadline", (timeLeft, want) => {
    expect(filter(all, { timeLeft })).toEqual(want);
  });

  it("filters by the supplier collected from", () => {
    const deck = errand("deck", { supplier: { id: "s-deck", name: "The Deck", location: "FASS" } });
    expect(filter([mid, deck], { from: "s-deck" })).toEqual(["deck"]);
  });

  it("filters by one destination or a whole area", () => {
    const com3 = errand("com3", { deliveryLocation: "COM3 Lobby" });
    const rc4 = errand("rc4", { deliveryLocation: "RC4 lobby" });
    const gone = errand("gone", { deliveryLocation: "A place no longer listed" });
    const list = [mid, com3, rc4, gone];
    expect(filter(list, { to: "place:COM3 Lobby" })).toEqual(["com3"]);
    expect(filter(list, { to: "area:Business & Computing" })).toEqual(["mid", "com3"]);
    expect(filter(list, { to: "area:UTown residences" })).toEqual(["rc4"]);
  });

  it("applies every filter together", () => {
    const com3Soon = errand("x", {
      deliveryLocation: "COM3 Lobby",
      expiresAt: new Date(NOW + 10 * MIN).toISOString(),
    });
    expect(filter([soon, com3Soon], { timeLeft: "under30m", to: "place:COM3 Lobby" })).toEqual(["x"]);
  });
});

describe("isFiltered", () => {
  it("is false only with every filter off", () => {
    expect(isFiltered(NO_FILTERS)).toBe(false);
    expect(isFiltered({ ...NO_FILTERS, timeLeft: "over1h" })).toBe(true);
    expect(isFiltered({ ...NO_FILTERS, from: "s" })).toBe(true);
    expect(isFiltered({ ...NO_FILTERS, to: "area:UTown" })).toBe(true);
  });
});

describe("pickupOptions", () => {
  it("lists each supplier once, by name", () => {
    const a = errand("a", { supplier: { id: "2", name: "Zebra Cafe", location: "" } });
    const b = errand("b", { supplier: { id: "1", name: "Apple Store", location: "" } });
    const c = errand("c", { supplier: { id: "2", name: "Zebra Cafe", location: "" } });
    expect(pickupOptions([a, b, c])).toEqual([
      { id: "1", name: "Apple Store" },
      { id: "2", name: "Zebra Cafe" },
    ]);
  });
});

describe("destinationGroups", () => {
  it("groups places by area, keeping order-service's order", () => {
    expect(destinationGroups(LOCATIONS)).toEqual([
      { area: "Business & Computing", places: ["COM1 Lobby", "COM3 Lobby"] },
      { area: "UTown residences", places: ["RC4 lobby"] },
    ]);
    expect(destinationGroups([])).toEqual([]);
  });
});

describe("ShownLog", () => {
  it("reports each errand only the first time it is seen", () => {
    const log = new ShownLog();
    expect(log.firstSighting("a")).toBe(true);
    expect(log.firstSighting("a")).toBe(false);
    expect(log.firstSighting("b")).toBe(true);
    expect(new ShownLog().firstSighting("a")).toBe(true);
  });
});
