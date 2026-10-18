// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-18
// Scope: Tests for hasNewerError, which decides when a success toast gives
//   way to an error that appeared on the page after it.
// Author review: James Teo — reviewed and verified.

import { describe, expect, it } from "vitest";
import { hasNewerError } from "./Toast";

/** Errors on the page, keyed by a stand-in for their element. */
const page = (entries: [string, string][]) => new Map(entries);

describe("hasNewerError", () => {
  it("is false with no errors before or after", () => {
    expect(hasNewerError(page([]), page([]))).toBe(false);
  });

  it("is true for an error that appeared after the success", () => {
    expect(hasNewerError(page([]), page([["post", "You already have 5 open errands."]]))).toBe(true);
  });

  it("is false for an error that was already showing, unchanged", () => {
    const before = page([["list", "Couldn't refresh the list."]]);
    expect(hasNewerError(before, page([["list", "Couldn't refresh the list."]]))).toBe(false);
  });

  it("is true when an error already showing changes its text", () => {
    const before = page([["step", ""]]);
    expect(hasNewerError(before, page([["step", "You can't suspend the last active admin."]]))).toBe(true);
  });

  it("is true for a new error beside one that was already showing", () => {
    const before = page([["list", "Couldn't refresh the list."]]);
    const now = page([
      ["list", "Couldn't refresh the list."],
      ["card", "This errand is no longer open."],
    ]);
    expect(hasNewerError(before, now)).toBe(true);
  });

  it("is false for an empty alert region, new or not", () => {
    expect(hasNewerError(page([]), page([["region", ""]]))).toBe(false);
  });

  it("is false when an error goes away", () => {
    expect(hasNewerError(page([["list", "Couldn't refresh the list."]]), page([]))).toBe(false);
  });
});
