// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-18
// Scope: Tests for the poller, with fake timers and a fake visibility source:
//   runs at start and on the interval, never overlaps, waits while the tab is
//   hidden, keeps going after a null answer or an error, pauses on a 401
//   until refresh(), queues a refresh made mid-run and drops the stale
//   answer of the run it interrupted, and stops.
// Author review: James Teo — reviewed and verified.

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { documentVisibility, startBurst, startPolling, type Poller, type Visibility } from "./polling";

const INTERVAL = 5_000;

/** A visibility source the test flips by hand. */
function fakeVisibility(initiallyVisible = true) {
  let visible = initiallyVisible;
  const listeners = new Set<() => void>();
  const visibility: Visibility = {
    isVisible: () => visible,
    subscribe(listener) {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
  };
  return {
    visibility,
    show() {
      visible = true;
      listeners.forEach((l) => l());
    },
    hide() {
      visible = false;
      listeners.forEach((l) => l());
    },
    listenerCount: () => listeners.size,
  };
}

/**
 * A task whose runs stay in flight until the test settles them. `seen` keeps
 * the isActive() the poller last passed in.
 */
function heldTask() {
  const runs: { resolve: (v: unknown) => void; reject: (e: unknown) => void }[] = [];
  const seen: { isActive?: () => boolean } = {};
  const task = vi.fn(
    (isActive: () => boolean) =>
      new Promise((resolve, reject) => {
        seen.isActive = isActive;
        runs.push({ resolve, reject });
      }),
  );
  return { task, runs, seen };
}

/** Lets settled promises run their callbacks without moving the clock. */
async function flush() {
  await vi.advanceTimersByTimeAsync(0);
}

let poller: Poller | null = null;

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  poller?.stop();
  poller = null;
  vi.useRealTimers();
});

describe("startPolling", () => {
  it("runs once at start, then on every interval", async () => {
    const task = vi.fn(async () => "list");
    poller = startPolling(task, INTERVAL, fakeVisibility().visibility);
    expect(task).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(INTERVAL);
    expect(task).toHaveBeenCalledTimes(2);
    await vi.advanceTimersByTimeAsync(2 * INTERVAL);
    expect(task).toHaveBeenCalledTimes(4);
  });

  it("never starts a run while the last one is still going", async () => {
    const { task, runs } = heldTask();
    poller = startPolling(task, INTERVAL, fakeVisibility().visibility);

    await vi.advanceTimersByTimeAsync(3 * INTERVAL);
    expect(task).toHaveBeenCalledTimes(1);

    runs[0]!.resolve("list");
    await flush();
    await vi.advanceTimersByTimeAsync(INTERVAL);
    expect(task).toHaveBeenCalledTimes(2);
  });

  it("waits while the tab is hidden and runs at once when it is shown", async () => {
    const task = vi.fn(async () => "list");
    const tab = fakeVisibility();
    poller = startPolling(task, INTERVAL, tab.visibility);
    await flush();

    tab.hide();
    await vi.advanceTimersByTimeAsync(4 * INTERVAL);
    expect(task).toHaveBeenCalledTimes(1);

    tab.show();
    expect(task).toHaveBeenCalledTimes(2);
  });

  it("keeps polling after a null answer, such as no credit account yet", async () => {
    const task = vi.fn(async () => null);
    poller = startPolling(task, INTERVAL, fakeVisibility().visibility);
    await vi.advanceTimersByTimeAsync(2 * INTERVAL);
    expect(task).toHaveBeenCalledTimes(3);
  });

  it("keeps polling after a failure other than 401", async () => {
    const task = vi.fn(async () => {
      throw { status: 503 };
    });
    poller = startPolling(task, INTERVAL, fakeVisibility().visibility);
    await vi.advanceTimersByTimeAsync(2 * INTERVAL);
    expect(task).toHaveBeenCalledTimes(3);
  });

  it("pauses after a 401 until refresh() resumes it", async () => {
    let status = 401;
    const task = vi.fn(async () => {
      if (status === 401) throw { status };
      return "list";
    });
    const tab = fakeVisibility();
    poller = startPolling(task, INTERVAL, tab.visibility);
    await flush();

    // Neither the interval nor the tab coming back asks again.
    await vi.advanceTimersByTimeAsync(3 * INTERVAL);
    tab.hide();
    tab.show();
    expect(task).toHaveBeenCalledTimes(1);

    // The user's next action does, and polling carries on from there.
    status = 200;
    poller.refresh();
    expect(task).toHaveBeenCalledTimes(2);
    await vi.advanceTimersByTimeAsync(INTERVAL);
    expect(task).toHaveBeenCalledTimes(3);
  });

  it("resumes when a refresh() queued behind a 401 run succeeds", async () => {
    const { task, runs } = heldTask();
    poller = startPolling(task, INTERVAL, fakeVisibility().visibility);

    poller.refresh();
    runs[0]!.reject({ status: 401 });
    await flush();
    expect(task).toHaveBeenCalledTimes(2);

    runs[1]!.resolve("list");
    await flush();
    await vi.advanceTimersByTimeAsync(INTERVAL);
    expect(task).toHaveBeenCalledTimes(3);
  });

  it("pauses on a 401 the task throws before returning a promise", async () => {
    const task = vi.fn(() => {
      throw { status: 401 };
    });
    poller = startPolling(task, INTERVAL, fakeVisibility().visibility);
    await vi.advanceTimersByTimeAsync(2 * INTERVAL);
    expect(task).toHaveBeenCalledTimes(1);
  });

  it("runs once more after the current run when refresh() is called mid-run", async () => {
    const { task, runs } = heldTask();
    poller = startPolling(task, INTERVAL, fakeVisibility().visibility);

    poller.refresh();
    poller.refresh();
    expect(task).toHaveBeenCalledTimes(1);

    runs[0]!.resolve("before the action");
    await flush();
    expect(task).toHaveBeenCalledTimes(2);

    runs[1]!.resolve("after the action");
    await flush();
    expect(task).toHaveBeenCalledTimes(2);
  });

  it("marks the run in flight stale on refresh(), so its old answer is dropped", async () => {
    const actives: (() => boolean)[] = [];
    const runs: { resolve: (v: unknown) => void }[] = [];
    const task = vi.fn(
      (isActive: () => boolean) =>
        new Promise((resolve) => {
          actives.push(isActive);
          runs.push({ resolve });
        }),
    );
    poller = startPolling(task, INTERVAL, fakeVisibility().visibility);
    expect(actives[0]!()).toBe(true);

    // The user's action lands while the first run is still waiting.
    poller.refresh();
    expect(actives[0]!()).toBe(false);

    runs[0]!.resolve("from before the action");
    await flush();
    expect(task).toHaveBeenCalledTimes(2);
    expect(actives[1]!()).toBe(true);
  });

  it("leaves a run started by refresh() active until the next refresh()", async () => {
    const actives: (() => boolean)[] = [];
    const task = vi.fn(async (isActive: () => boolean) => {
      actives.push(isActive);
    });
    poller = startPolling(task, INTERVAL, fakeVisibility().visibility);
    await flush();

    poller.refresh();
    await flush();
    expect(actives[1]!()).toBe(true);
    await vi.advanceTimersByTimeAsync(INTERVAL);
    expect(actives[1]!()).toBe(true);

    poller.refresh();
    expect(actives[1]!()).toBe(false);
  });

  it("stops: no more runs, no queued run, isActive() false, listener removed", async () => {
    const tab = fakeVisibility();
    const { task, runs, seen } = heldTask();
    poller = startPolling(task, INTERVAL, tab.visibility);
    expect(seen.isActive?.()).toBe(true);

    poller.refresh();
    poller.stop();
    expect(seen.isActive?.()).toBe(false);
    expect(tab.listenerCount()).toBe(0);

    runs[0]!.resolve("late");
    await vi.advanceTimersByTimeAsync(3 * INTERVAL);
    poller.refresh();
    expect(task).toHaveBeenCalledTimes(1);
  });
});

describe("startBurst", () => {
  const STEP = 2_000;
  let stop: (() => void) | null = null;

  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    stop?.();
    stop = null;
    vi.useRealTimers();
  });

  it("refreshes once per interval, at most the given number of times", () => {
    const refresh = vi.fn();
    stop = startBurst(refresh, () => false, STEP, 3);

    expect(refresh).not.toHaveBeenCalled();
    vi.advanceTimersByTime(STEP);
    expect(refresh).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(10 * STEP);
    expect(refresh).toHaveBeenCalledTimes(3);
  });

  it("stops early once settled", () => {
    const refresh = vi.fn();
    let settled = false;
    stop = startBurst(refresh, () => settled, STEP, 3);

    vi.advanceTimersByTime(STEP);
    settled = true;
    vi.advanceTimersByTime(10 * STEP);
    expect(refresh).toHaveBeenCalledTimes(1);
  });

  it("does nothing more once stopped", () => {
    const refresh = vi.fn();
    stop = startBurst(refresh, () => false, STEP, 3);

    stop();
    vi.advanceTimersByTime(10 * STEP);
    expect(refresh).not.toHaveBeenCalled();
  });
});

describe("documentVisibility", () => {
  it("is always visible outside a browser", () => {
    const visibility = documentVisibility();
    expect(visibility.isVisible()).toBe(true);
    expect(typeof visibility.subscribe(() => {})).toBe("function");
  });
});
