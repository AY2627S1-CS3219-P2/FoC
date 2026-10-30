// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: Asking the browser for the courier's location on Find errands
//   (D-098), and following it while the screen is open.
// Author review: PENDING — <reviewer to complete>

import { useCallback, useEffect, useRef, useState } from "react";
import type { LatLon } from "./nearby";

/**
 * Where the courier's location stands. `ask` is the state before the browser
 * has asked; asking happens only when the courier taps the button, so the
 * permission prompt is never sprung on them. The location stays in the page.
 */
export type CourierLocation =
  | { kind: "ask" }
  | { kind: "locating" }
  | { kind: "located"; at: LatLon }
  | { kind: "refused" }
  | { kind: "unavailable" };

/** A fix may be up to a minute old: a walker moves little in that time. */
const WATCH_OPTIONS: PositionOptions = { maximumAge: 60_000, timeout: 20_000 };

/**
 * The courier's location and `request`, which asks the browser for it. If the
 * browser already has permission from an earlier visit, it is fetched at once
 * without a prompt.
 */
export function useCourierLocation(): { location: CourierLocation; request: () => void } {
  const supported = typeof navigator !== "undefined" && "geolocation" in navigator;
  const [location, setLocation] = useState<CourierLocation>(
    supported ? { kind: "ask" } : { kind: "unavailable" },
  );
  const watch = useRef<number | null>(null);

  const request = useCallback(() => {
    if (!supported || watch.current !== null) return;
    setLocation({ kind: "locating" });
    watch.current = navigator.geolocation.watchPosition(
      (pos) => setLocation({ kind: "located", at: { lat: pos.coords.latitude, lon: pos.coords.longitude } }),
      (err) => {
        if (watch.current !== null) navigator.geolocation.clearWatch(watch.current);
        watch.current = null;
        setLocation({ kind: err.code === err.PERMISSION_DENIED ? "refused" : "unavailable" });
      },
      WATCH_OPTIONS,
    );
  }, [supported]);

  useEffect(() => {
    let active = true;
    // Only to skip the button when permission was already given; a browser
    // without the Permissions API simply shows the button.
    navigator.permissions
      ?.query({ name: "geolocation" })
      .then((status) => {
        if (!active) return;
        if (status.state === "granted") request();
        else if (status.state === "denied") setLocation({ kind: "refused" });
      })
      .catch(() => undefined);
    return () => {
      active = false;
      if (watch.current !== null) navigator.geolocation.clearWatch(watch.current);
      watch.current = null;
    };
  }, [request]);

  return { location, request };
}
