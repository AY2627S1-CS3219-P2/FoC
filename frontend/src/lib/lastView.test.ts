// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-22
// Scope: Tests for the per-tab view memory.
// Author review: Nigeltzy - AI built this implementation for testing based on my requirements. Seems valid as a test.

import { beforeEach, describe, expect, it, vi } from "vitest";
import { clearLastView, readLastView, writeLastView } from "./lastView";

/**
 * A minimal sessionStorage.
 *
 * vitest runs in node here and frontend/AGENTS.md scopes the test tier to pure
 * logic, so there is no DOM and no jsdom dependency. Stubbing the one API
 * under test keeps it that way — and it is what lets the "storage throws"
 * case below be written at all, which no real browser would let us stage.
 */
function fakeStorage() {
  const map = new Map<string, string>();
  return {
    getItem: (k: string) => map.get(k) ?? null,
    setItem: (k: string, v: string) => void map.set(k, v),
    removeItem: (k: string) => void map.delete(k),
  };
}

describe("lastView", () => {
  beforeEach(() => {
    vi.unstubAllGlobals();
    vi.stubGlobal("sessionStorage", fakeStorage());
  });

  it("round-trips a view", () => {
    writeLastView("profile");
    expect(readLastView()).toBe("profile");
  });

  it("returns null when nothing is stored", () => {
    expect(readLastView()).toBeNull();
  });

  it("rejects a value that is no longer a view", () => {
    // A stored name outlives deploys. A view that has since been renamed or
    // removed would otherwise render nothing at all — a blank shell.
    sessionStorage.setItem("foc.lastView", "errands-v1");
    expect(readLastView()).toBeNull();
  });

  it("forgets on clear", () => {
    writeLastView("credits");
    clearLastView();
    expect(readLastView()).toBeNull();
  });

  it("survives storage being unavailable", () => {
    // Private windows and blocked site data throw on access rather than
    // returning null. Losing the view is fine; a blank page is not.
    vi.stubGlobal("sessionStorage", {
      getItem() {
        throw new DOMException("denied");
      },
      setItem() {
        throw new DOMException("denied");
      },
      removeItem() {
        throw new DOMException("denied");
      },
    });

    expect(readLastView()).toBeNull();
    expect(() => writeLastView("suppliers")).not.toThrow();
    expect(() => clearLastView()).not.toThrow();
  });
});
