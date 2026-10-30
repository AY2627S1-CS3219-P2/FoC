// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: Tests for the batching activity reporter (D-092) and the rating
//   text (D-093).
// Author review: PENDING — <reviewer to complete>

import { describe, expect, it, vi } from "vitest";
import {
  createActivityReporter,
  SHOWN_BATCH_MAX,
  SHOWN_BATCH_MS,
  type Timer,
} from "./activityReporter";
import type { RecordedFilters, ShownItem } from "./orderTypes";
import { ratingText } from "./ratings";

const ANY: RecordedFilters = { timeLeft: "any", from: "", to: "" };
const UTOWN: RecordedFilters = { timeLeft: "under1h", from: "", to: "area:UTown" };

/** A timer that runs only when told to. */
function manualTimer() {
  let due: (() => void) | null = null;
  const timer: Timer = {
    set: (fn, ms) => {
      expect(ms).toBe(SHOWN_BATCH_MS);
      due = fn;
      return 1;
    },
    clear: () => {
      due = null;
    },
  };
  return { timer, fire: () => due?.(), armed: () => due !== null };
}

function stubApi() {
  return {
    recordShown: vi.fn<(items: ShownItem[], filters: RecordedFilters) => Promise<void>>(async () => undefined),
    recordSearch: vi.fn<(filters: RecordedFilters) => Promise<void>>(async () => undefined),
  };
}

describe("createActivityReporter", () => {
  it("gathers shown errands and sends them once the batch time has passed", () => {
    const api = stubApi();
    const t = manualTimer();
    const r = createActivityReporter(api, t.timer);

    r.shown({ errandId: "a", position: 0, filters: ANY });
    r.shown({ errandId: "b", position: 1, filters: ANY });
    expect(api.recordShown).not.toHaveBeenCalled();

    t.fire();
    expect(api.recordShown).toHaveBeenCalledTimes(1);
    expect(api.recordShown).toHaveBeenCalledWith(
      [
        { errandId: "a", position: 0 },
        { errandId: "b", position: 1 },
      ],
      ANY,
    );
    expect(t.armed()).toBe(false);
  });

  it("sends one request per set of filters", () => {
    const api = stubApi();
    const t = manualTimer();
    const r = createActivityReporter(api, t.timer);

    r.shown({ errandId: "a", position: 0, filters: ANY });
    r.shown({ errandId: "b", position: 0, filters: UTOWN });
    r.shown({ errandId: "c", position: 1, filters: ANY });
    r.flush();

    expect(api.recordShown).toHaveBeenCalledTimes(2);
    expect(api.recordShown).toHaveBeenCalledWith(
      [
        { errandId: "a", position: 0 },
        { errandId: "c", position: 1 },
      ],
      ANY,
    );
    expect(api.recordShown).toHaveBeenCalledWith([{ errandId: "b", position: 0 }], UTOWN);
  });

  it("splits a batch larger than order-service takes", () => {
    const api = stubApi();
    const r = createActivityReporter(api, manualTimer().timer);
    for (let i = 0; i < SHOWN_BATCH_MAX + 5; i++) r.shown({ errandId: `e${i}`, position: i % 100, filters: ANY });

    r.flush();

    expect(api.recordShown).toHaveBeenCalledTimes(2);
    expect(api.recordShown.mock.calls[0]![0]).toHaveLength(SHOWN_BATCH_MAX);
    expect(api.recordShown.mock.calls[1]![0]).toHaveLength(5);
  });

  it("sends nothing when nothing is queued, and searches at once", () => {
    const api = stubApi();
    const r = createActivityReporter(api, manualTimer().timer);

    r.flush();
    r.search(UTOWN);

    expect(api.recordShown).not.toHaveBeenCalled();
    expect(api.recordSearch).toHaveBeenCalledWith(UTOWN);
  });

  it("drops a failed send without throwing", async () => {
    const api = {
      recordShown: vi.fn(async () => Promise.reject(new Error("offline"))),
      recordSearch: vi.fn(async () => Promise.reject(new Error("offline"))),
    };
    const r = createActivityReporter(api, manualTimer().timer);

    r.shown({ errandId: "a", position: 0, filters: ANY });
    expect(() => r.flush()).not.toThrow();
    expect(() => r.search(ANY)).not.toThrow();
    // Let the rejected promises settle; an unhandled rejection would fail the run.
    await Promise.resolve();
  });
});

describe("ratingText", () => {
  it.each([
    [{ count: 0 }, "No ratings yet"],
    [{ count: 1, average: 5 }, "5.0 (1 rating)"],
    [{ count: 12, average: 4.25 }, "4.3 (12 ratings)"],
  ])("%j reads %s", (rating, text) => {
    expect(ratingText(rating)).toBe(text);
  });
});
