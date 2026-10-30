// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-31
// Scope: Tests for the scheduling helpers against order-service's recorded
//   window (a future time, from 00:00 UTC on the day before it), and the
//   date-and-time box's text to and from an instant in any device time zone.
// Author review: PENDING — <reviewer to complete>

import { describe, expect, it } from "vitest";
import {
  fromLocalInput,
  sameMinute,
  scheduleHint,
  scheduleLimit,
  scheduleProblem,
  toLocalInput,
  toScheduled,
} from "./schedule";
import { formatDateTime } from "./time";

const MINUTE = 60_000;
const NOW = Date.parse("2026-10-30T10:00:00Z");

describe("scheduleLimit", () => {
  it("is 00:00 UTC two days on, so today's and tomorrow's UTC dates are open", () => {
    expect(new Date(scheduleLimit(NOW)).toISOString()).toBe("2026-11-01T00:00:00.000Z");
  });

  it("goes by the UTC date, not the time of day", () => {
    expect(scheduleLimit(Date.parse("2026-10-30T00:00:00Z"))).toBe(scheduleLimit(NOW));
    expect(scheduleLimit(Date.parse("2026-10-30T23:59:59Z"))).toBe(scheduleLimit(NOW));
  });

  it("rolls over a month's end", () => {
    expect(new Date(scheduleLimit(Date.parse("2026-12-31T12:00:00Z"))).toISOString()).toBe(
      "2027-01-02T00:00:00.000Z",
    );
  });
});

describe("toLocalInput and fromLocalInput", () => {
  it("round-trip an instant to the minute in the device's time zone", () => {
    const at = Date.parse("2026-10-31T06:05:00Z");
    expect(toLocalInput(at)).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/);
    expect(fromLocalInput(toLocalInput(at))).toBe(at);
  });

  it("drops seconds when writing the box", () => {
    const at = Date.parse("2026-10-31T06:05:42Z");
    expect(fromLocalInput(toLocalInput(at))).toBe(at - 42_000);
  });

  it("reads a box that includes seconds", () => {
    const at = Date.parse("2026-10-31T06:05:00Z");
    expect(fromLocalInput(`${toLocalInput(at)}:30`)).toBe(at + 30_000);
  });

  it.each(["", "tomorrow", "2026-10-31", "2026-10-31 14:00", "2026-10-31T14:00Z"])(
    "reads %j as nothing",
    (text) => {
      expect(fromLocalInput(text)).toBeNull();
    },
  );
});

describe("toScheduled", () => {
  it("sends the box's time as an RFC 3339 UTC timestamp", () => {
    const at = Date.parse("2026-10-31T06:05:00Z");
    expect(toScheduled(toLocalInput(at))).toBe("2026-10-31T06:05:00.000Z");
  });

  it("is null for an empty box", () => {
    expect(toScheduled("")).toBeNull();
  });
});

describe("sameMinute", () => {
  it("ignores seconds", () => {
    expect(sameMinute(NOW, NOW + 59_000)).toBe(true);
    expect(sameMinute(NOW, NOW + MINUTE)).toBe(false);
  });
});

describe("scheduleProblem", () => {
  const box = (at: number) => toLocalInput(at);

  it("takes a time inside the window", () => {
    expect(scheduleProblem(box(NOW + 2 * MINUTE), NOW)).toBeNull();
    expect(scheduleProblem(box(scheduleLimit(NOW) - MINUTE), NOW)).toBeNull();
  });

  it("wants a time", () => {
    expect(scheduleProblem("", NOW)).toBe("Choose the date and time it opens.");
  });

  it("refuses a time that has passed, or is this minute", () => {
    expect(scheduleProblem(box(NOW - MINUTE), NOW)).toBe("Choose a time that hasn't passed yet.");
    expect(scheduleProblem(box(NOW), NOW)).toBe("Choose a time that hasn't passed yet.");
  });

  it("refuses a time on or after the limit, naming it", () => {
    const limit = formatDateTime(new Date(scheduleLimit(NOW)).toISOString());
    expect(scheduleProblem(box(scheduleLimit(NOW)), NOW)).toBe(`Choose a time before ${limit}.`);
  });
});

describe("scheduleHint", () => {
  it("names the limit and says couriers won't see it until then", () => {
    const limit = formatDateTime(new Date(scheduleLimit(NOW)).toISOString());
    expect(scheduleHint(NOW)).toBe(`Pick a time before ${limit}. Couriers won't see it until then.`);
  });
});
