// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-09-27
// Scope: Tests for the supplier-service fixture used when
//   VITE_USE_FIXTURES=true.
// Author review: nigeltzy

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { SupplierApiError } from "./suppliersApi";
import { createSuppliersFixture } from "./suppliersFixture";
import type { SupplierInput } from "./types";

/** Runs the fixture's simulated latency to completion. */
async function settle<T>(promise: Promise<T>): Promise<T> {
  await vi.runAllTimersAsync();
  return promise;
}

const INPUT: SupplierInput = {
  name: "Test Kiosk",
  type: "Food",
  building: "COM1",
  floor: "1",
  location_description: "Near the lift",
  latitude: 1.2949,
  longitude: 103.7737,
  opening_time: "09:00",
  closing_time: "17:00",
  image_url: "",
  description: "",
  is_available: true,
};

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

describe("createSuppliersFixture", () => {
  it("lists the 21 seeded suppliers in name order", async () => {
    const list = await settle(createSuppliersFixture().listSuppliers());

    expect(list).toHaveLength(21);
    const names = list.map((s) => s.name);
    expect(names).toEqual([...names].sort((a, b) => a.localeCompare(b)));
    // Hours are converted from the CSV's "0900hrs" form, as seed.go does.
    expect(list.every((s) => /^\d\d:\d\d$/.test(s.opening_time))).toBe(true);
  });

  it("matches the category against the whole type, ignoring case", async () => {
    const list = await settle(createSuppliersFixture().listSuppliers({ category: "food" }));

    expect(list.length).toBeGreaterThan(0);
    // "Food/Coffee" is a different type, not a match for "food".
    expect(list.every((s) => s.type === "Food")).toBe(true);
  });

  it("matches the search as part of the name, ignoring case", async () => {
    const list = await settle(createSuppliersFixture().listSuppliers({ search: "COFFEE" }));

    expect(list.length).toBeGreaterThan(0);
    expect(list.every((s) => s.name.toLowerCase().includes("coffee"))).toBe(true);
  });

  it("stores what is created, updated and deleted", async () => {
    const api = createSuppliersFixture();

    const created = await settle(api.createSupplier(INPUT));
    expect(await settle(api.getSupplier(created.id))).toMatchObject(INPUT);

    const updated = await settle(api.updateSupplier(created.id, { ...INPUT, floor: "2" }));
    expect(updated.floor).toBe("2");
    expect(updated.created_at).toBe(created.created_at);

    await settle(api.deleteSupplier(created.id));
    expect(await settle(api.listSuppliers())).toHaveLength(21);
  });

  it("answers an unknown id with supplier-service's 404", async () => {
    const error = await createSuppliersFixture()
      .getSupplier("no-such-id")
      .catch((err: unknown) => err);

    expect(error).toBeInstanceOf(SupplierApiError);
    expect(error).toMatchObject({ status: 404, message: "supplier not found" });
  });

  it("starts every fixture from the seed", async () => {
    await settle(createSuppliersFixture().createSupplier(INPUT));

    expect(await settle(createSuppliersFixture().listSuppliers())).toHaveLength(21);
  });
});
