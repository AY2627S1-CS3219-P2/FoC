// AI Assistance Disclosure:
// Tool: Codex (model: GPT-6), date: 2026-10-28
// Scope: Regression tests against the scheduled field recorded in
//   order-service/AGENTS.md and api/openapi.yaml; no implementation changes.
// Author review: PENDING

import { describe, expect, it, vi } from "vitest";
import { createOrdersApi } from "./ordersApi";
import { STATE_LABEL } from "./format";
import type { AuthorizedSend } from "../auth/session";

const scheduled = "2026-10-29T14:00:00Z";
const input = {
  supplierId: "00000000-0000-4000-8000-000000000001",
  itemDescription: "Coffee",
  deliveryLocation: "COM3 Lobby",
  reward: 5,
  scheduled,
};

function client() {
  const send = vi.fn<AuthorizedSend>(async () => ({
    ok: true,
    status: 200,
    body: {
      errand: { id: "id", state: "SCHEDULED", version: 1, supplier: {}, actions: ["amend", "cancel"], scheduled },
      history: [],
      serverTime: "2026-10-28T14:00:00Z",
    },
  }));
  return { send, api: createOrdersApi(send) };
}

describe("scheduled errands API compatibility", () => {
  it("sends the scheduled timestamp on creation", async () => {
    const { send, api } = client();
    await api.createErrand(input, "scheduled-create");
    expect(send.mock.calls[0]![0].body).toHaveProperty("scheduled", scheduled);
  });

  it.each([scheduled, ""])("sends schedule amendment %j", async (value) => {
    const { send, api } = client();
    const changes = { scheduled: value, itemDescription: "Coffee" };
    await api.amendErrand("id", 1, changes);
    expect(send.mock.calls[0]![0].body).toHaveProperty("scheduled", value);
  });

  it("provides a visible state label for a scheduled errand", () => {
    expect(Reflect.get(STATE_LABEL, "SCHEDULED")).toBe("Scheduled");
  });
});
