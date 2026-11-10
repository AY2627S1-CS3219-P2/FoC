// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-05
// Scope: Tests for the Notifications screen's list logic and for what a poll
//   announces (F5.1, F7.6.1).
// Author review: PENDING — <reviewer to complete>

import { describe, expect, it } from "vitest";
import { pollOutcome } from "./newNotifications";
import type { Notification, NotificationPage } from "./notificationsApi";
import { markedRead, withLatestPage, withNextPage } from "./NotificationsView";

function note(id: string, read = false): Notification {
  return {
    id,
    errandId: "errand",
    stateChange: "errand.accepted",
    message: `message ${id}`,
    read,
    createdAt: "2026-11-05T08:30:00Z",
  };
}

function page(ids: string[], total = ids.length): NotificationPage {
  return { notifications: ids.map((id) => note(id)), total };
}

const idsOf = (p: NotificationPage) => p.notifications.map((n) => n.id);

describe("withLatestPage", () => {
  it("shows the first page as it is", () => {
    expect(withLatestPage(null, page(["b", "a"]))).toEqual(page(["b", "a"]));
  });

  it("keeps rows Load more added behind a fresh first page", () => {
    const merged = withLatestPage(page(["b", "a"], 3), page(["c", "b"], 3));
    expect(idsOf(merged)).toEqual(["c", "b", "a"]);
    expect(merged.total).toBe(3);
  });

  it("takes the read status from the fresh copy", () => {
    const fresh: NotificationPage = { notifications: [note("a", true)], total: 1 };
    expect(withLatestPage(page(["a"]), fresh).notifications[0].read).toBe(true);
  });
});

describe("withNextPage", () => {
  it("adds older rows behind the ones shown", () => {
    expect(idsOf(withNextPage(page(["c", "b"], 3), page(["a"], 3)))).toEqual(["c", "b", "a"]);
  });

  it("skips rows a new notification pushed onto the next page", () => {
    expect(idsOf(withNextPage(page(["c", "b"], 4), page(["b", "a"], 4)))).toEqual(["c", "b", "a"]);
  });
});

describe("markedRead", () => {
  it("marks only that notification", () => {
    const after = markedRead(page(["b", "a"]), "a");
    expect(after.notifications.map((n) => n.read)).toEqual([false, true]);
  });
});

describe("pollOutcome", () => {
  it("announces nothing on the first poll of a session", () => {
    const first = pollOutcome(null, [note("a")]);
    expect(first.message).toBeNull();
    expect(first.seen).toEqual(new Set(["a"]));
  });

  it("announces one new notification in its own words", () => {
    expect(pollOutcome(new Set(["a"]), [note("b"), note("a")]).message).toBe("message b");
  });

  it("counts several new notifications", () => {
    const outcome = pollOutcome(new Set(["a"]), [note("c"), note("b"), note("a")]);
    expect(outcome.message).toBe("You have 2 new notifications.");
  });

  it("stays quiet when nothing is new, or what is new is already read", () => {
    expect(pollOutcome(new Set(["a"]), [note("a")]).message).toBeNull();
    expect(pollOutcome(new Set(["a"]), [note("b", true)]).message).toBeNull();
  });

  it("does not announce the same notification twice", () => {
    const first = pollOutcome(new Set<string>(), [note("a")]);
    expect(first.message).toBe("message a");
    expect(pollOutcome(first.seen, [note("a")]).message).toBeNull();
  });
});
