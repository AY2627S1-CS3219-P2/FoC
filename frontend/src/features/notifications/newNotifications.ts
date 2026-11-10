// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-05
// Scope: Works out what to tell the user when a poll finds notifications they
//   have not seen, for the message F7.6.1 asks for.
// Author review: nigeltzy - Checked that the implementation is as expected and seems to be correct.

import type { Notification } from "./notificationsApi";

/** What one poll found: the ids to remember, and the message to show, if any. */
export interface PollOutcome {
  seen: Set<string>;
  message: string | null;
}

/**
 * Compares a poll with the ids already seen. `seen` is null on the first poll
 * of a session: what is already there is not news, so nothing is announced.
 * A notification already read, say on another device, is not announced either.
 */
export function pollOutcome(seen: Set<string> | null, latest: Notification[]): PollOutcome {
  const ids = new Set(latest.map((n) => n.id));
  if (seen === null) return { seen: ids, message: null };

  const fresh = latest.filter((n) => !seen.has(n.id) && !n.read);
  const remembered = new Set([...seen, ...ids]);
  if (fresh.length === 0) return { seen: remembered, message: null };
  if (fresh.length === 1) return { seen: remembered, message: fresh[0].message };
  return { seen: remembered, message: `You have ${fresh.length} new notifications.` };
}
