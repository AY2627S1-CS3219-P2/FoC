// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-18
// Scope: Polling for screens that must stay current without a reload (F7.5):
//   a pure poller that runs a task on an interval while the tab is visible,
//   never overlaps runs, drops a run's result once refresh() supersedes it,
//   and pauses after a 401, plus the hook that ties one to a component's
//   lifetime.
// Author review: James Teo — reviewed and verified.

import { useCallback, useEffect, useRef, type DependencyList } from "react";

/**
 * order-service has no push channel, so a list or an open detail stays
 * current by asking again every few seconds while the user can see it.
 *
 * - The task runs once at start, then every `intervalMs` while the tab is
 *   visible, and straight away when the tab becomes visible again.
 * - A tick is skipped while the previous run is still going, so runs never
 *   overlap. refresh() instead queues one run after the current one, so the
 *   refetch after a user's action always reads the state after it, and marks
 *   the run in flight stale: its answer may predate the action, so it is
 *   dropped rather than drawn over the action's result.
 * - A run that ends in a 401 pauses the poller. The transport has already
 *   refreshed and retried by then, so asking again would only spend another
 *   refresh; refresh() (the user's next action) resumes it, and a new session
 *   mounts new pollers.
 * - Any other outcome, a null "no account yet" answer or an error included,
 *   keeps the poller going. The task shows its own result.
 */

/** Whether the page is on screen, and a way to hear when that changes. */
export interface Visibility {
  isVisible(): boolean;
  /** Calls `listener` on every change and returns the unsubscribe. */
  subscribe(listener: () => void): () => void;
}

/**
 * The browser tab's visibility. Outside a browser it is always visible and
 * never changes.
 */
export function documentVisibility(): Visibility {
  if (typeof document === "undefined") {
    return { isVisible: () => true, subscribe: () => () => {} };
  }
  return {
    isVisible: () => document.visibilityState === "visible",
    subscribe(listener) {
      document.addEventListener("visibilitychange", listener);
      return () => document.removeEventListener("visibilitychange", listener);
    },
  };
}

/**
 * One poll. `isActive()` turns false once the poller is stopped, so a result
 * that lands after the screen moved on is dropped rather than shown, and once
 * refresh() is called after the run started, so an answer from before the
 * user's action never replaces the one after it. Let a failure reject (or
 * rethrow it after showing it) so a 401 can pause polling.
 */
export type PollTask = (isActive: () => boolean) => Promise<unknown>;

export interface Poller {
  /**
   * Runs the task now, or right after the run in flight, and resumes a
   * paused poller. For after the user's own actions: the run in flight, if
   * any, is marked stale and its result dropped.
   */
  refresh(): void;
  /** Stops for good: no more runs, and `isActive()` turns false. */
  stop(): void;
}

/** Starts polling `task` every `intervalMs` while `visibility` says the page is visible. */
export function startPolling(task: PollTask, intervalMs: number, visibility: Visibility): Poller {
  let stopped = false;
  let running = false;
  let queued = false;
  let paused = false;
  // Bumped by refresh(): a run started under an older generation is stale.
  let generation = 0;

  function run(): void {
    running = true;
    const started = generation;
    const isActive = () => !stopped && started === generation;
    let attempt: Promise<unknown>;
    try {
      attempt = Promise.resolve(task(isActive));
    } catch (err) {
      attempt = Promise.reject(err);
    }
    // Only refresh() and its queued run happen while paused, so the latest
    // outcome decides: a 401 pauses, anything else resumes.
    attempt
      .then(
        () => {
          paused = false;
        },
        (err: unknown) => {
          paused = isUnauthorized(err);
        },
      )
      .finally(() => {
        running = false;
        if (queued && !stopped) {
          queued = false;
          run();
        }
      });
  }

  /** A scheduled run: skipped while hidden, paused or already running. */
  function tick(): void {
    if (stopped || paused || running || !visibility.isVisible()) return;
    run();
  }

  const timer = setInterval(tick, intervalMs);
  const unsubscribe = visibility.subscribe(tick);
  run();

  return {
    refresh() {
      if (stopped) return;
      paused = false;
      generation += 1;
      if (running) queued = true;
      else run();
    },
    stop() {
      if (stopped) return;
      stopped = true;
      queued = false;
      clearInterval(timer);
      unsubscribe();
    },
  };
}

/** Every client's error carries the HTTP status; 401 is the one that pauses. */
function isUnauthorized(err: unknown): boolean {
  return typeof err === "object" && err !== null && (err as { status?: unknown }).status === 401;
}

/**
 * Polls `task` every `intervalMs` while the component is mounted and the tab
 * visible, and returns refresh() for after the user's own actions. A null
 * task polls nothing, for a screen whose client is absent.
 *
 * The poller restarts when `deps` change, like an effect's dependencies, so
 * pass what the task reads to choose its request (the client, a scope). The
 * old poller's `isActive()` then turns false and its late result is dropped.
 * The latest `task` is always the one run, so it may close over state freely.
 */
export function usePolling(
  task: PollTask | null,
  intervalMs: number,
  deps: DependencyList,
): () => void {
  const taskRef = useRef(task);
  const pollerRef = useRef<Poller | null>(null);

  useEffect(() => {
    taskRef.current = task;
  });

  const enabled = task !== null;
  useEffect(() => {
    if (!enabled) return;
    const poller = startPolling(
      (isActive) => (taskRef.current ? taskRef.current(isActive) : Promise.resolve()),
      intervalMs,
      documentVisibility(),
    );
    pollerRef.current = poller;
    return () => {
      poller.stop();
      if (pollerRef.current === poller) pollerRef.current = null;
    };
    // The caller's deps choose when to restart; see the doc comment.
  }, [enabled, intervalMs, ...deps]);

  return useCallback(() => pollerRef.current?.refresh(), []);
}
