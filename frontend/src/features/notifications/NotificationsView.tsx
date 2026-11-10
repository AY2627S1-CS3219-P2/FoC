// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-05
// Scope: The Notifications screen (F5.1, F7.3.1, D-145): the user's list, newest
//   first, refreshed every 30 seconds, with read status, mark as read, Load
//   more and an empty state, in the style of the Credits screen.
// Author review: PENDING — <reviewer to complete>

import { useState } from "react";
import { usePolling } from "../../lib/polling";
import type { ViewName } from "../../views";
import type { Notification, NotificationPage, NotificationsApi } from "./notificationsApi";

/** How often the list is fetched again: the refresh interval F7.5.1 asks for (D-145). */
export const NOTIFICATIONS_POLL_MS = 30_000;

export const PAGE_SIZE = 50;

/**
 * The fresh first page in front of everything already loaded. Rows on the fresh
 * page replace their older copies, so a read status stays current.
 */
export function withLatestPage(
  current: NotificationPage | null,
  page: NotificationPage,
): NotificationPage {
  if (!current) return page;
  const fresh = new Set(page.notifications.map((n) => n.id));
  return {
    notifications: [...page.notifications, ...current.notifications.filter((n) => !fresh.has(n.id))],
    total: page.total,
  };
}

/**
 * A further page after the rows shown. New notifications push older ones down,
 * so a later page can repeat rows already shown; those are skipped.
 */
export function withNextPage(current: NotificationPage, page: NotificationPage): NotificationPage {
  const shown = new Set(current.notifications.map((n) => n.id));
  return {
    notifications: [...current.notifications, ...page.notifications.filter((n) => !shown.has(n.id))],
    total: page.total,
  };
}

/** `current` with one notification marked read. */
export function markedRead(current: NotificationPage, id: string): NotificationPage {
  return {
    ...current,
    notifications: current.notifications.map((n) => (n.id === id ? { ...n, read: true } : n)),
  };
}

interface NotificationsViewProps {
  api: NotificationsApi;
  /** Where the empty state's button goes: the screen errands are followed on. */
  primaryView: ViewName;
  primaryLabel: string;
  onNavigate: (view: ViewName) => void;
}

export function NotificationsView({
  api,
  primaryView,
  primaryLabel,
  onNavigate,
}: NotificationsViewProps) {
  const [page, setPage] = useState<NotificationPage | null>(null);
  const [failed, setFailed] = useState<string | null>(null);
  const [loadingMore, setLoadingMore] = useState(false);

  const refresh = usePolling(
    async (isActive) => {
      try {
        const latest = await api.list(PAGE_SIZE, 0);
        if (!isActive()) return;
        setPage((current) => withLatestPage(current, latest));
        setFailed(null);
      } catch (err) {
        if (isActive()) setFailed("Notifications are unavailable right now.");
        throw err;
      }
    },
    NOTIFICATIONS_POLL_MS,
    [api],
  );

  async function loadMore() {
    if (!page) return;
    setLoadingMore(true);
    setFailed(null);
    try {
      const next = await api.list(PAGE_SIZE, page.notifications.length);
      setPage((current) => (current ? withNextPage(current, next) : current));
    } catch {
      setFailed("Couldn't load more notifications.");
    } finally {
      setLoadingMore(false);
    }
  }

  async function markRead(id: string) {
    setFailed(null);
    try {
      await api.markRead(id);
      setPage((current) => (current ? markedRead(current, id) : current));
    } catch {
      setFailed("Couldn't mark that notification as read.");
    }
  }

  const hasMore = page !== null && page.notifications.length < page.total;

  return (
    <section>
      <div className="page-head">
        <h1>Notifications</h1>
        <p className="page-sub">Updates on the errands you&rsquo;re part of.</p>
      </div>

      {page === null && !failed && (
        <p className="status" role="status">
          Loading notifications…
        </p>
      )}

      {page !== null && page.notifications.length === 0 && (
        <div className="status empty-state" role="status">
          <p>No notifications yet. You&rsquo;ll hear here when one of your errands changes.</p>
          <button type="button" className="btn btn-primary" onClick={() => onNavigate(primaryView)}>
            {primaryLabel}
          </button>
        </div>
      )}

      {page !== null && page.notifications.length > 0 && (
        <div className="ledger">
          {page.notifications.map((n) => (
            <NotificationRow key={n.id} notification={n} onMarkRead={() => void markRead(n.id)} />
          ))}
        </div>
      )}

      {failed && (
        <p className="status error" role="alert">
          {failed} Try again in a moment.{" "}
          <button type="button" className="link-btn" onClick={refresh}>
            Retry
          </button>
        </p>
      )}

      {hasMore && (
        <div className="btn-row start">
          <button
            type="button"
            className="btn btn-secondary"
            onClick={() => void loadMore()}
            disabled={loadingMore}
          >
            {loadingMore ? "Loading…" : "Load more"}
          </button>
        </div>
      )}
    </section>
  );
}

function NotificationRow({
  notification,
  onMarkRead,
}: {
  notification: Notification;
  onMarkRead: () => void;
}) {
  return (
    <div className={`ledger-row ${notification.read ? "" : "notification-unread"}`}>
      <div>
        <div className="ledger-label">{notification.message}</div>
        <div className="ledger-time">{formatWhen(notification.createdAt)}</div>
      </div>
      {notification.read ? (
        <span className="ledger-time">Read</span>
      ) : (
        <button type="button" className="link-btn" onClick={onMarkRead}>
          Mark as read
        </button>
      )}
    </div>
  );
}

function formatWhen(iso: string): string {
  return new Date(iso).toLocaleString("en-SG", {
    day: "numeric",
    month: "short",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });
}
