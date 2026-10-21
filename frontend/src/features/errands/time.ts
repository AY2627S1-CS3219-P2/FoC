// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-18
// Scope: Countdown and elapsed-time helpers for errands, worked out against
//   order-service's serverTime rather than the device clock, their duration
//   and date formats, and a once-a-second clock hook to drive them.
// Author review: James Teo — reviewed and verified.

import { useEffect, useState } from "react";

/**
 * A device clock can be minutes off. Every order-service response carries
 * `serverTime`, so a screen stores the offset between the two when a response
 * arrives and reads deadlines against the server's clock from then on:
 *
 *   const offset = clockOffset(list.serverTime);
 *   const left = countdown(errand.expiresAt, serverNow(offset));
 *
 * The functions below take the current time as an argument and read no clock
 * themselves, so they are pure. useNow() is the one clock, ticking the screen
 * that shows them.
 */

/**
 * How far the server's clock is ahead of this device's, in milliseconds
 * (negative when it is behind). An unreadable `serverTime` gives 0, which
 * falls back to the device clock.
 */
export function clockOffset(serverTime: string, deviceNow: number = Date.now()): number {
  const server = Date.parse(serverTime);
  return Number.isNaN(server) ? 0 : server - deviceNow;
}

/** The server's current time, from the device's and the stored offset. */
export function serverNow(offset: number, deviceNow: number = Date.now()): number {
  return deviceNow + offset;
}

/**
 * Milliseconds from `now` until `deadline`, negative once it has passed. Null
 * when there is no deadline or it cannot be read.
 */
export function msUntil(deadline: string | undefined, now: number): number | null {
  if (!deadline) return null;
  const at = Date.parse(deadline);
  return Number.isNaN(at) ? null : at - now;
}

/**
 * A duration as the screens write it: "1 h 52 min", "3 h", "4 min", "45 s".
 * Takes whole seconds; anything below zero reads as "0 s".
 */
export function formatDuration(totalSeconds: number): string {
  const seconds = Math.max(0, Math.floor(totalSeconds));
  if (seconds < 60) return `${seconds} s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes} min`;
  const hours = Math.floor(minutes / 60);
  const rest = minutes % 60;
  return rest ? `${hours} h ${rest} min` : `${hours} h`;
}

/**
 * Time left before a deadline. `due` once it reaches zero, so a screen shows
 * "Expiring…" or "Confirming…" instead of a negative time: order-service's
 * sweeper acts a few seconds after the deadline, and the next poll then shows
 * the new state.
 */
export type Countdown = { due: false; text: string } | { due: true };

/**
 * The countdown to `deadline` at `now` (server time), or null when there is
 * no deadline to count down to. Seconds round up, so the last second reads
 * "1 s" rather than "0 s" before the countdown becomes due.
 */
export function countdown(deadline: string | undefined, now: number): Countdown | null {
  const ms = msUntil(deadline, now);
  if (ms === null) return null;
  if (ms <= 0) return { due: true };
  return { due: false, text: formatDuration(Math.ceil(ms / 1000)) };
}

/**
 * How long ago `since` was at `now` (server time), e.g. "3 h" for an admin's
 * "In this state for 3 h". A time slightly in the future reads "0 s". Null
 * when there is no time or it cannot be read.
 */
export function elapsed(since: string | undefined, now: number): string | null {
  const ms = msUntil(since, now);
  if (ms === null) return null;
  return formatDuration(Math.floor(-ms / 1000));
}

/**
 * A date and time as "8 Oct, 19:06", the format the Credits screen already
 * uses, in the device's time zone unless `timeZone` names one.
 */
export function formatDateTime(iso: string, timeZone?: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "";
  const day = date.toLocaleDateString("en-SG", { day: "numeric", month: "short", timeZone });
  const time = date.toLocaleTimeString("en-SG", {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
    timeZone,
  });
  return `${day}, ${time}`;
}

/**
 * The device's time, updated every `intervalMs` while the component is
 * mounted: one per visible screen drives every countdown on it. Combine it
 * with serverNow(offset, now) for deadlines.
 */
export function useNow(intervalMs = 1000): number {
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(timer);
  }, [intervalMs]);
  return now;
}
