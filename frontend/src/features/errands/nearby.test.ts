// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: Tests for the default ranking by pickup distance (D-098).
// Author review: PENDING — <reviewer to complete>

import { describe, expect, it } from "vitest";
import { withDistance } from "./FindErrandsView";
import { distanceMetres, formatDistance, rankByDistance, type LatLon } from "./nearby";
import type { Errand } from "./orderTypes";

function errand(id: string, supplierId: string): Errand {
  return {
    id,
    state: "OPEN",
    version: 1,
    supplier: { id: supplierId, name: supplierId, location: "" },
    itemDescription: id,
    deliveryLocation: "COM1 Lobby",
    reward: 1,
    createdAt: "2026-10-29T09:00:00Z",
    updatedAt: "2026-10-29T09:00:00Z",
    stateChangedAt: "2026-10-29T09:00:00Z",
    actions: ["accept"],
  };
}

// Points on campus, read off OpenStreetMap.
const COM3: LatLon = { lat: 1.29492, lon: 103.775011 };
const BIZ2: LatLon = { lat: 1.293242, lon: 103.775068 };
const UTOWN: LatLon = { lat: 1.303535, lon: 103.774431 };

describe("distanceMetres", () => {
  it("is zero to itself, symmetric, and about 111 km per degree of latitude", () => {
    expect(distanceMetres(COM3, COM3)).toBe(0);
    expect(distanceMetres(COM3, UTOWN)).toBeCloseTo(distanceMetres(UTOWN, COM3), 9);
    expect(distanceMetres({ lat: 0, lon: 0 }, { lat: 1, lon: 0 })).toBeCloseTo(111_195, -1);
  });

  it("puts BIZ 2 nearer COM 3 than UTown is", () => {
    expect(distanceMetres(COM3, BIZ2)).toBeLessThan(250);
    expect(distanceMetres(COM3, UTOWN)).toBeGreaterThan(900);
  });
});

describe("rankByDistance", () => {
  const pickups = new Map([
    ["far", UTOWN],
    ["near", BIZ2],
  ]);
  const list = [errand("a", "far"), errand("b", "unknown"), errand("c", "near"), errand("d", "far")];

  it("puts the nearest pickup first and unknown pickups last, keeping ties in list order", () => {
    const ranked = rankByDistance(list, COM3, pickups);
    expect(ranked.map((p) => p.errand.id)).toEqual(["c", "a", "d", "b"]);
    expect(ranked[0].metres).toBeLessThan(250);
    expect(ranked[3].metres).toBeNull();
  });

  it("leaves the order alone, with no distances, when the courier's location is unknown", () => {
    const ranked = rankByDistance(list, null, pickups);
    expect(ranked.map((p) => p.errand.id)).toEqual(["a", "b", "c", "d"]);
    expect(ranked.every((p) => p.metres === null)).toBe(true);
  });

  it("does not reorder the list it was given", () => {
    rankByDistance(list, COM3, pickups);
    expect(list.map((e) => e.id)).toEqual(["a", "b", "c", "d"]);
  });
});

describe("formatDistance", () => {
  it.each([
    [3, "10 m"],
    [84, "80 m"],
    [955, "960 m"],
    [999.4, "1000 m"],
    [1_234, "1.2 km"],
    [12_400, "12 km"],
  ])("%d m reads %s", (metres, text) => {
    expect(formatDistance(metres)).toBe(text);
  });
});

describe("withDistance", () => {
  it("adds the pickup's distance to the card line when it is known", () => {
    expect(withDistance("Expires in 40 min", 350)).toBe("Expires in 40 min · Pickup 350 m away");
    expect(withDistance(null, 1_500)).toBe("Pickup 1.5 km away");
    expect(withDistance("Expires in 40 min", null)).toBe("Expires in 40 min");
    expect(withDistance(null, null)).toBeNull();
  });
});
