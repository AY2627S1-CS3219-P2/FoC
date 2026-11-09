// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-09
// Scope: Tests for nextListIndex, the keyboard movement of the workspace lists.
// Author review: PENDING — <reviewer to complete>

import { describe, expect, it } from "vitest";
import { nextListIndex } from "./listKeys";

describe("nextListIndex", () => {
  it("moves one row at a time and stops at the ends", () => {
    expect(nextListIndex(1, "ArrowDown", 4)).toBe(2);
    expect(nextListIndex(3, "ArrowDown", 4)).toBe(3);
    expect(nextListIndex(1, "ArrowUp", 4)).toBe(0);
    expect(nextListIndex(0, "ArrowUp", 4)).toBe(0);
  });

  it("jumps to the first and last rows", () => {
    expect(nextListIndex(2, "Home", 4)).toBe(0);
    expect(nextListIndex(1, "End", 4)).toBe(3);
  });

  it("starts from the top going down and the bottom going up when nothing is focused", () => {
    expect(nextListIndex(-1, "ArrowDown", 4)).toBe(0);
    expect(nextListIndex(-1, "ArrowUp", 4)).toBe(3);
  });

  it("ignores other keys and empty lists", () => {
    expect(nextListIndex(1, "Enter", 4)).toBeNull();
    expect(nextListIndex(1, "a", 4)).toBeNull();
    expect(nextListIndex(-1, "ArrowDown", 0)).toBeNull();
  });
});
