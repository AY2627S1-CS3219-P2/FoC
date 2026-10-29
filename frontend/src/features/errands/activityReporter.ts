// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: Sending Find errands' shown and search records to order-service
//   (D-092), shown ones gathered into batches.
// Author review: PENDING — <reviewer to complete>

import type { OrdersApi } from "./ordersApi";
import type { RecordedFilters, ShownItem } from "./orderTypes";

/** How long shown errands gather before they go out in one request. */
export const SHOWN_BATCH_MS = 2_000;

/** order-service takes at most this many errands in one shown record. */
export const SHOWN_BATCH_MAX = 100;

export interface ActivityReporter {
  /** Queues one shown errand with the filters set when it was seen. */
  shown(item: ShownItem & { filters: RecordedFilters }): void;
  /** Sends a search at once. */
  search(filters: RecordedFilters): void;
  /** Sends whatever is queued now, for when the screen closes. */
  flush(): void;
}

/** The timer the reporter uses, injectable so tests need not wait. */
export interface Timer {
  set(fn: () => void, ms: number): unknown;
  clear(handle: unknown): void;
}

const realTimer: Timer = {
  set: (fn, ms) => setTimeout(fn, ms),
  clear: (handle) => clearTimeout(handle as ReturnType<typeof setTimeout>),
};

/**
 * Builds the reporter. Shown errands are sent in batches, one request per set
 * of filters, so scrolling a long list costs a request every two seconds at
 * most rather than one per card. A failed send is dropped: these records feed
 * ranking, and losing a few must never get in the courier's way.
 */
export function createActivityReporter(
  api: Pick<OrdersApi, "recordShown" | "recordSearch">,
  timer: Timer = realTimer,
): ActivityReporter {
  let queue: (ShownItem & { filters: RecordedFilters })[] = [];
  let pending: unknown = null;

  function flush() {
    if (pending !== null) timer.clear(pending);
    pending = null;
    const batch = queue;
    queue = [];
    const byFilters = new Map<string, { filters: RecordedFilters; items: ShownItem[] }>();
    for (const { filters, errandId, position } of batch) {
      const key = JSON.stringify([filters.timeLeft, filters.from, filters.to]);
      const group = byFilters.get(key) ?? { filters, items: [] };
      group.items.push({ errandId, position });
      byFilters.set(key, group);
    }
    for (const { filters, items } of byFilters.values()) {
      for (let i = 0; i < items.length; i += SHOWN_BATCH_MAX) {
        api.recordShown(items.slice(i, i + SHOWN_BATCH_MAX), filters).catch(() => undefined);
      }
    }
  }

  return {
    shown(item) {
      queue.push(item);
      if (pending === null) pending = timer.set(flush, SHOWN_BATCH_MS);
    },
    search(filters) {
      api.recordSearch(filters).catch(() => undefined);
    },
    flush,
  };
}
