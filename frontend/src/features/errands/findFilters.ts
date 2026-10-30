// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: Find errands' filters (D-092): time left, where an errand is
//   collected from and where it is delivered to, applied to the open list
//   in the browser.
// Author review: PENDING — <reviewer to complete>

import type { CampusLocation, Errand } from "./orderTypes";
import { msUntil } from "./time";

/**
 * How much time an errand has left before it expires, as the courier picks
 * it. The bands are the screen's own; order-service filters nothing.
 */
export const TIME_LEFT_OPTIONS = [
  { value: "any", label: "Any time left" },
  { value: "under30m", label: "Ends within 30 min" },
  { value: "under1h", label: "Ends within 1 h" },
  { value: "over1h", label: "Over 1 h left" },
] as const;

export type TimeLeft = (typeof TIME_LEFT_OPTIONS)[number]["value"];

/**
 * The filters on Find errands. `from` is a supplier's id, or "" for any.
 * `to` is "" for any destination, `area:<area>` for every place in an area,
 * or `place:<name>` for one delivery location.
 */
export interface FindFilters {
  timeLeft: TimeLeft;
  from: string;
  to: string;
}

export const NO_FILTERS: FindFilters = { timeLeft: "any", from: "", to: "" };

const MINUTE = 60_000;

export function isFiltered(filters: FindFilters): boolean {
  return filters.timeLeft !== "any" || filters.from !== "" || filters.to !== "";
}

/** The errands that pass every filter, in the order given. */
export function filterErrands(
  errands: Errand[],
  filters: FindFilters,
  now: number,
  locations: CampusLocation[],
): Errand[] {
  const areaOf = new Map(locations.map((l) => [l.name, l.area]));
  return errands.filter(
    (e) =>
      passesTimeLeft(e, filters.timeLeft, now) &&
      (filters.from === "" || e.supplier.id === filters.from) &&
      passesDestination(e, filters.to, areaOf),
  );
}

function passesTimeLeft(errand: Errand, timeLeft: TimeLeft, now: number): boolean {
  if (timeLeft === "any") return true;
  const left = msUntil(errand.expiresAt, now);
  // An errand without a readable deadline cannot be placed in a band.
  if (left === null) return false;
  switch (timeLeft) {
    case "under30m":
      return left <= 30 * MINUTE;
    case "under1h":
      return left <= 60 * MINUTE;
    case "over1h":
      return left > 60 * MINUTE;
  }
}

function passesDestination(errand: Errand, to: string, areaOf: Map<string, string>): boolean {
  if (to === "") return true;
  if (to.startsWith("place:")) return errand.deliveryLocation === to.slice("place:".length);
  if (to.startsWith("area:")) return areaOf.get(errand.deliveryLocation) === to.slice("area:".length);
  return true;
}

/** One pickup a courier can filter by: a supplier with errands on the list. */
export interface PickupOption {
  id: string;
  name: string;
}

/** The suppliers the list's errands collect from, once each, by name. */
export function pickupOptions(errands: Errand[]): PickupOption[] {
  const byId = new Map<string, string>();
  for (const e of errands) byId.set(e.supplier.id, e.supplier.name);
  return [...byId]
    .map(([id, name]) => ({ id, name }))
    .sort((a, b) => a.name.localeCompare(b.name) || a.id.localeCompare(b.id));
}

/** The delivery locations grouped by area, in the order order-service lists them. */
export function destinationGroups(locations: CampusLocation[]): { area: string; places: string[] }[] {
  const groups: { area: string; places: string[] }[] = [];
  for (const l of locations) {
    const last = groups[groups.length - 1];
    if (last && last.area === l.area) last.places.push(l.name);
    else groups.push({ area: l.area, places: [l.name] });
  }
  return groups;
}
