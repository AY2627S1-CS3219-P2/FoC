// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-31
// Scope: The scheduling helpers behind the errand form: the latest time a
//   schedule may be set for, the date-and-time box's text to and from an
//   instant, and the form's check, all from order-service's recorded
//   scheduled-errand rules (order-service/AGENTS.md, PR #67).
// Author review: PENDING — <reviewer to complete>

import { formatDateTime } from "./time";

/**
 * The first instant a schedule may no longer be set for at `now`: 00:00 UTC
 * two days on. order-service takes a time only from 00:00 UTC on the calendar
 * day before it, so today's and tomorrow's UTC dates are open and the day
 * after is not. Its check is the one that counts; this one is UX.
 */
export function scheduleLimit(now: number): number {
  const today = new Date(now);
  return Date.UTC(today.getUTCFullYear(), today.getUTCMonth(), today.getUTCDate() + 2);
}

/**
 * An instant as a date-and-time box holds it, "2026-10-31T14:05", in the
 * device's time zone, to the minute.
 */
export function toLocalInput(at: number): string {
  const d = new Date(at);
  const pad = (n: number) => String(n).padStart(2, "0");
  return (
    `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}` +
    `T${pad(d.getHours())}:${pad(d.getMinutes())}`
  );
}

/**
 * The instant a date-and-time box's text names, in the device's time zone,
 * or null when it is blank or unreadable. Parsed by hand: `Date.parse` of a
 * zoneless time is not read the same way by every browser.
 */
export function fromLocalInput(text: string): number | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})(?::(\d{2}))?$/.exec(text);
  if (!m) return null;
  const [, year, month, day, hour, minute, second] = m;
  const at = new Date(
    Number(year),
    Number(month) - 1,
    Number(day),
    Number(hour),
    Number(minute),
    Number(second ?? 0),
  ).getTime();
  return Number.isNaN(at) ? null : at;
}

/** The box's text as order-service's `scheduled`: an RFC 3339 UTC timestamp. */
export function toScheduled(text: string): string | null {
  const at = fromLocalInput(text);
  return at === null ? null : new Date(at).toISOString();
}

/** Whether two timestamps name the same minute; the box holds no seconds. */
export function sameMinute(a: number, b: number): boolean {
  return Math.floor(a / 60_000) === Math.floor(b / 60_000);
}

/** The hint under the box: the latest time it takes, and what scheduling means. */
export function scheduleHint(now: number): string {
  const limit = new Date(scheduleLimit(now)).toISOString();
  return `Pick a time before ${formatDateTime(limit)}. Couriers won't see it until then.`;
}

/** What is wrong with the box's text at `now`, or null when it will do. */
export function scheduleProblem(text: string, now: number): string | null {
  const at = fromLocalInput(text);
  if (at === null) return "Choose the date and time it opens.";
  if (at <= now) return "Choose a time that hasn't passed yet.";
  const limit = scheduleLimit(now);
  if (at >= limit) return `Choose a time before ${formatDateTime(new Date(limit).toISOString())}.`;
  return null;
}
