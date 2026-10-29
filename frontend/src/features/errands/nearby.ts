// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: The default ranking on Find errands (D-086, D-098): distance from
//   the courier's location to each errand's pickup, worked out in the
//   browser, nearest first.
// Author review: PENDING — <reviewer to complete>

import type { Errand } from "./orderTypes";

/** A position in degrees. */
export interface LatLon {
  lat: number;
  lon: number;
}

/** How Find errands orders its list. "nearest" needs the courier's location. */
export type SortOrder = "nearest" | "newest";

const EARTH_RADIUS_M = 6_371_008.8;

/** The great-circle distance between a and b, in metres. */
export function distanceMetres(a: LatLon, b: LatLon): number {
  const rad = Math.PI / 180;
  const dLat = (b.lat - a.lat) * rad;
  const dLon = (b.lon - a.lon) * rad;
  const h =
    Math.sin(dLat / 2) ** 2 + Math.cos(a.lat * rad) * Math.cos(b.lat * rad) * Math.sin(dLon / 2) ** 2;
  return 2 * EARTH_RADIUS_M * Math.asin(Math.min(1, Math.sqrt(h)));
}

/** An errand and how far its pickup is from the courier, when known. */
export interface Placed {
  errand: Errand;
  metres: number | null;
}

/**
 * The errands with their pickup distance from `from`, nearest first. An errand
 * whose supplier has no known position keeps its place relative to the others
 * like it, after every errand that has one. With no `from`, the order is left
 * as given and every distance is null.
 */
export function rankByDistance(
  errands: Errand[],
  from: LatLon | null,
  pickups: ReadonlyMap<string, LatLon>,
): Placed[] {
  const placed = errands.map((errand) => {
    const at = pickups.get(errand.supplier.id);
    return { errand, metres: from && at ? distanceMetres(from, at) : null };
  });
  if (!from) return placed;
  // Array.prototype.sort is stable, so equal distances keep the list's order.
  return placed.sort((a, b) => {
    if (a.metres === null || b.metres === null) {
      return (a.metres === null ? 1 : 0) - (b.metres === null ? 1 : 0);
    }
    return a.metres - b.metres;
  });
}

/** "80 m", "950 m", "1.2 km", "12 km": rounded as a walker reads it. */
export function formatDistance(metres: number): string {
  if (metres < 1000) return `${Math.max(10, Math.round(metres / 10) * 10)} m`;
  const km = metres / 1000;
  return `${km < 10 ? km.toFixed(1) : Math.round(km)} km`;
}

// AI-generated (edited by PENDING): D-094's suggested errand first.
/** The list with the errand `id` moved to the top, the rest in order; unchanged when `id` is absent or not on it. */
export function withFirst(placed: Placed[], id: string | undefined): Placed[] {
  const i = id === undefined ? -1 : placed.findIndex((p) => p.errand.id === id);
  if (i <= 0) return placed;
  return [placed[i]!, ...placed.slice(0, i), ...placed.slice(i + 1)];
}
