// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-18
// Scope: Tests for the order-service client — the path, method, headers and
//   body of every call, the response checks, how failures surface, and the
//   request key — against a stubbed authorized transport, plus two calls
//   through lib/http's real send() with a stubbed fetch.
// Author review: James Teo — reviewed and verified.

import { afterEach, describe, expect, it, vi } from "vitest";
import type { AuthorizedSend } from "../auth/session";
import { NetworkError, send, type HttpResponse } from "../../lib/http";
import { createOrdersApi, newRequestId, OrdersApiError, type OrdersApi } from "./ordersApi";
import type { CreateErrandRequest, Errand, ErrandDetail } from "./orderTypes";

const ID = "6b1f0c8e-2a3d-4e5f-9a0b-1c2d3e4f5a6b";
const SUPPLIER_ID = "0d9c8b7a-6f5e-4d3c-2b1a-0f9e8d7c6b5a";
const SERVER_TIME = "2026-10-18T09:00:00Z";

/** An OPEN errand as order-service sends it to its requester. */
const ERRAND: Errand = {
  id: ID,
  state: "OPEN",
  version: 3,
  supplier: { id: SUPPLIER_ID, name: "Spinelli Coffee", location: "COM3" },
  itemDescription: "1 × Iced latte, less ice",
  deliveryLocation: "COM3 Lobby",
  deliveryNote: "Grey backpack",
  reward: 10,
  requesterId: "3f2b8c1e-1d2a-4b5c-8d9e-0a1b2c3d4e5f",
  createdAt: "2026-10-18T08:30:00Z",
  updatedAt: "2026-10-18T08:30:00Z",
  stateChangedAt: "2026-10-18T08:30:00Z",
  expiresAt: "2026-10-18T10:30:00Z",
  actions: ["amend", "cancel"],
};

const DETAIL: ErrandDetail = {
  errand: ERRAND,
  history: [{ action: "CREATE", toState: "OPEN", occurredAt: "2026-10-18T08:30:00Z" }],
  serverTime: SERVER_TIME,
};

const LIST = { items: [ERRAND], serverTime: SERVER_TIME };

const INPUT: CreateErrandRequest = {
  supplierId: SUPPLIER_ID,
  itemDescription: "1 × Iced latte, less ice",
  deliveryLocation: "COM3 Lobby",
  reward: 10,
};

function ok(body: unknown, status = 200): HttpResponse {
  return { ok: true, status, body };
}

function fail(status: number, body: unknown = null): HttpResponse {
  return { ok: false, status, body };
}

/** A transport that records each request and answers with `response`. */
function stubSend(response: HttpResponse | Error) {
  const send = vi.fn<AuthorizedSend>(async () => {
    if (response instanceof Error) throw response;
    return response;
  });
  return { send, api: createOrdersApi(send) };
}

/** The options of the only request made. */
function sent(send: ReturnType<typeof stubSend>["send"]) {
  expect(send).toHaveBeenCalledTimes(1);
  return send.mock.calls[0]![0];
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("ordersApi reads", () => {
  const reads: [string, (api: OrdersApi) => Promise<unknown>, string, unknown][] = [
    ["listLocations", (api) => api.listLocations(), "/api/orders/locations", { items: [{ name: "COM3 Lobby", area: "Computing" }] }],
    ["listOpen", (api) => api.listOpen(), "/api/orders/open", LIST],
    [
      "listRequested active",
      (api) => api.listRequested("active"),
      "/api/orders/requested?scope=active",
      LIST,
    ],
    [
      "listRequested history",
      (api) => api.listRequested("history"),
      "/api/orders/requested?scope=history",
      LIST,
    ],
    [
      "listAssigned active",
      (api) => api.listAssigned("active"),
      "/api/orders/assigned?scope=active",
      { ...LIST, history: [] },
    ],
    [
      "listAssigned history",
      (api) => api.listAssigned("history"),
      "/api/orders/assigned?scope=history",
      { items: [], history: [], serverTime: SERVER_TIME },
    ],
    ["listActiveForAdmin", (api) => api.listActiveForAdmin(), "/api/orders?scope=active", LIST],
    ["getErrand", (api) => api.getErrand(ID), `/api/orders/${ID}`, DETAIL],
  ];

  it.each(reads)("%s sends GET to its path with no body or headers", async (_name, call, path, body) => {
    const { send, api } = stubSend(ok(body));
    await call(api);
    const options = sent(send);
    expect(options.path).toBe(path);
    expect(options.method ?? "GET").toBe("GET");
    expect(options.body).toBeUndefined();
    expect(options.headers).toBeUndefined();
  });

  it("goes to the gateway's base URL", async () => {
    const { send, api } = stubSend(ok(LIST));
    await api.listOpen();
    expect(sent(send).baseUrl).toBe("");
  });

  it("returns the delivery locations as order-service lists them", async () => {
    const items = [
      { name: "COM3 Lobby", area: "Computing" },
      { name: "The Deck, AS8", area: "Arts & Social Sciences" },
    ];
    const { api } = stubSend(ok({ items }));
    await expect(api.listLocations()).resolves.toEqual(items);
  });

  it("returns a list with its serverTime", async () => {
    const { api } = stubSend(ok(LIST));
    await expect(api.listOpen()).resolves.toEqual(LIST);
  });

  it("returns the courier's history records", async () => {
    const record = {
      errandId: ID,
      outcome: "RELEASED",
      endedAt: "2026-10-18T08:45:00Z",
      supplier: ERRAND.supplier,
      itemDescription: ERRAND.itemDescription,
      deliveryLocation: ERRAND.deliveryLocation,
      reward: 10,
    };
    const { api } = stubSend(ok({ items: [], history: [record], serverTime: SERVER_TIME }));
    const list = await api.listAssigned("history");
    expect(list.items).toEqual([]);
    expect(list.history).toEqual([record]);
  });

  it("returns the errand, its history and serverTime as sent", async () => {
    const { api } = stubSend(ok(DETAIL));
    await expect(api.getErrand(ID)).resolves.toEqual(DETAIL);
  });

  it("treats a detail without a history array as an empty history", async () => {
    const { api } = stubSend(ok({ errand: ERRAND, serverTime: SERVER_TIME }));
    expect((await api.getErrand(ID)).history).toEqual([]);
  });
});

describe("ordersApi create", () => {
  it("posts to /api/orders with the request key in X-Request-Id", async () => {
    const { send, api } = stubSend(ok(DETAIL, 201));
    await api.createErrand(INPUT, "key-1");
    const options = sent(send);
    expect(options).toMatchObject({ path: "/api/orders", method: "POST" });
    expect(options.headers).toEqual({ "X-Request-Id": "key-1" });
    expect(options.body).toEqual(INPUT);
  });

  it("sends the note when there is one", async () => {
    const { send, api } = stubSend(ok(DETAIL, 201));
    await api.createErrand({ ...INPUT, deliveryNote: "Grey backpack" }, "key-1");
    expect(sent(send).body).toEqual({ ...INPUT, deliveryNote: "Grey backpack" });
  });

  it("sends only the contract's fields, dropping unknown and undefined ones", async () => {
    const { send, api } = stubSend(ok(DETAIL, 201));
    const withExtras = { ...INPUT, deliveryNote: undefined, supplierName: "Spinelli Coffee" };
    await api.createErrand(withExtras as CreateErrandRequest, "key-1");
    expect(sent(send).body).toEqual(INPUT);
  });

  it.each([201, 200])("resolves a %i with the errand", async (status) => {
    // 200 is order-service's answer to a key it has already seen: the errand
    // that key created, not a second one.
    const { api } = stubSend(ok(DETAIL, status));
    await expect(api.createErrand(INPUT, "key-1")).resolves.toEqual(DETAIL);
  });
});

describe("ordersApi amend and actions", () => {
  it("amends with PATCH, the version and only the changed fields", async () => {
    const { send, api } = stubSend(ok(DETAIL));
    await api.amendErrand(ID, 3, { reward: 15 });
    const options = sent(send);
    expect(options).toMatchObject({ path: `/api/orders/${ID}`, method: "PATCH" });
    expect(options.body).toEqual({ version: 3, reward: 15 });
    expect(options.headers).toBeUndefined();
  });

  it("keeps an empty note, which clears it, and drops undefined and unknown fields", async () => {
    const { send, api } = stubSend(ok(DETAIL));
    const changes = { deliveryNote: "", itemDescription: undefined, version: 9, state: "OPEN" };
    await api.amendErrand(ID, 3, changes as Parameters<OrdersApi["amendErrand"]>[2]);
    expect(sent(send).body).toEqual({ version: 3, deliveryNote: "" });
  });

  it("accepts with POST and the version", async () => {
    const { send, api } = stubSend(ok(DETAIL));
    await api.accept(ID, 3);
    const options = sent(send);
    expect(options).toMatchObject({ path: `/api/orders/${ID}/accept`, method: "POST" });
    expect(options.body).toEqual({ version: 3 });
    expect(options.headers).toBeUndefined();
  });

  const actions: [string, (api: OrdersApi) => Promise<ErrandDetail>, string][] = [
    ["withdraw", (api) => api.withdraw(ID), "withdraw"],
    ["pickUp", (api) => api.pickUp(ID), "pickup"],
    ["deliver", (api) => api.deliver(ID), "deliver"],
    ["confirm", (api) => api.confirm(ID), "confirm"],
    ["cancel", (api) => api.cancel(ID), "cancel"],
    ["complete", (api) => api.complete(ID), "complete"],
  ];

  it.each(actions)("%s sends POST to /{id}/%s with no body", async (_name, call, route) => {
    const { send, api } = stubSend(ok(DETAIL));
    await expect(call(api)).resolves.toEqual(DETAIL);
    const options = sent(send);
    expect(options).toMatchObject({ path: `/api/orders/${ID}/${route}`, method: "POST" });
    expect(options.body).toBeUndefined();
    expect(options.headers).toBeUndefined();
  });

  it("encodes the errand ID into the path", async () => {
    const { send, api } = stubSend(ok(DETAIL));
    await api.getErrand("a/b");
    expect(sent(send).path).toBe("/api/orders/a%2Fb");

    const second = stubSend(ok(DETAIL));
    await second.api.accept("a/b?x", 1);
    expect(sent(second.send).path).toBe("/api/orders/a%2Fb%3Fx/accept");
  });
});

describe("ordersApi through lib/http", () => {
  /** Stubs fetch to answer `body` and returns the mock, to read what was sent. */
  function stubFetch(body: unknown, status = 200) {
    const fetchMock = vi.fn(
      async (_url: string, _init?: RequestInit) =>
        new Response(JSON.stringify(body), {
          status,
          headers: { "Content-Type": "application/json" },
        }),
    );
    vi.stubGlobal("fetch", fetchMock);
    return fetchMock;
  }

  it("sends an amend to fetch as a PATCH with a JSON body", async () => {
    const fetchMock = stubFetch(DETAIL);
    await createOrdersApi((options) => send(options)).amendErrand(ID, 3, { reward: 15 });
    const [url, init] = fetchMock.mock.calls[0]!;
    expect(url).toBe(`/api/orders/${ID}`);
    expect(init?.method).toBe("PATCH");
    expect(new Headers(init?.headers).get("Content-Type")).toBe("application/json");
    expect(JSON.parse(String(init?.body))).toEqual({ version: 3, reward: 15 });
  });

  it("puts the request key on the create's X-Request-Id header", async () => {
    const fetchMock = stubFetch(DETAIL, 201);
    await createOrdersApi((options) => send(options)).createErrand(INPUT, "key-1");
    const [, init] = fetchMock.mock.calls[0]!;
    expect(init?.method).toBe("POST");
    expect(new Headers(init?.headers).get("X-Request-Id")).toBe("key-1");
  });
});

describe("ordersApi responses that are not what the spec promises", () => {
  const malformed: [string, (api: OrdersApi) => Promise<unknown>, unknown][] = [
    ["a detail with no errand", (api) => api.getErrand(ID), { history: [], serverTime: SERVER_TIME }],
    [
      "a detail whose errand has no actions",
      (api) => api.getErrand(ID),
      { errand: { ...ERRAND, actions: undefined }, history: [], serverTime: SERVER_TIME },
    ],
    ["a detail with no serverTime", (api) => api.cancel(ID), { errand: ERRAND, history: [] }],
    ["a list with no items", (api) => api.listOpen(), { serverTime: SERVER_TIME }],
    [
      "a list holding something that is not an errand",
      (api) => api.listRequested("active"),
      { items: [{ id: ID }], serverTime: SERVER_TIME },
    ],
    [
      "an assigned list with no history",
      (api) => api.listAssigned("active"),
      { items: [], serverTime: SERVER_TIME },
    ],
    ["locations that are not text", (api) => api.listLocations(), { items: [1, 2] }],
    ["locations in the old list-of-names shape", (api) => api.listLocations(), { items: ["COM3 Lobby"] }],
    ["a location without its area", (api) => api.listLocations(), { items: [{ name: "COM3 Lobby" }] }],
    ["an empty body", (api) => api.createErrand(INPUT, "key-1"), null],
  ];

  it.each(malformed)("turns %s into an OrdersApiError with status 0", async (_name, call, body) => {
    const { api } = stubSend(ok(body));
    const err = await call(api).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(OrdersApiError);
    expect((err as OrdersApiError).status).toBe(0);
  });
});

describe("ordersApi failures", () => {
  it("keeps order-service's error message and status", async () => {
    const { api } = stubSend(fail(409, { error: "someone else accepted this errand first" }));
    await expect(api.accept(ID, 3)).rejects.toMatchObject({
      name: "OrdersApiError",
      message: "someone else accepted this errand first",
      status: 409,
    });
  });

  it("keeps a 422 from create as it is", async () => {
    const { api } = stubSend(fail(422, { error: "not enough available credits for this reward" }));
    await expect(api.createErrand(INPUT, "key-1")).rejects.toMatchObject({ status: 422 });
  });

  it("falls back to the status when the body has no error field", async () => {
    const { api } = stubSend(fail(502, null));
    await expect(api.listOpen()).rejects.toThrow("Request failed (502)");
  });

  it("turns a request that never arrived into status 0", async () => {
    const { api } = stubSend(new NetworkError(""));
    const err = await api.createErrand(INPUT, "key-1").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(OrdersApiError);
    expect((err as OrdersApiError).status).toBe(0);
  });

  it("lets any other thrown error through unchanged", async () => {
    const boom = new TypeError("boom");
    const { api } = stubSend(boom);
    await expect(api.listOpen()).rejects.toBe(boom);
  });
});

describe("newRequestId", () => {
  const UUID_V4 = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

  it("mints a fresh UUID within X-Request-Id's 64 characters each time", () => {
    const first = newRequestId();
    const second = newRequestId();
    expect(first).toMatch(UUID_V4);
    expect(first.length).toBeLessThanOrEqual(64);
    expect(second).not.toBe(first);
  });

  it("builds the same kind of UUID where randomUUID is missing", () => {
    // Over plain HTTP on another host, crypto.randomUUID is absent but
    // crypto.getRandomValues is not.
    const real = globalThis.crypto;
    vi.stubGlobal("crypto", {
      getRandomValues: (array: Uint8Array) => real.getRandomValues(array),
    });
    const first = newRequestId();
    expect(first).toMatch(UUID_V4);
    expect(newRequestId()).not.toBe(first);
  });
});

// AI-generated (edited by PENDING): the ratings (D-093) and Find errands'
// records (D-092).
describe("ordersApi ratings and records", () => {
  const RATING = {
    errandId: ID,
    raterId: "3f2b8c1e-1d2a-4b5c-8d9e-0a1b2c3d4e5f",
    rateeId: "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d",
    raterRole: "requester",
    stars: 4,
    ratedAt: SERVER_TIME,
  };
  const FILTERS = { timeLeft: "under1h", from: "", to: "area:UTown" } as const;

  it("rate posts the stars to the errand's rating", async () => {
    const { send, api } = stubSend(ok(RATING, 201));
    await expect(api.rate(ID, 4)).resolves.toEqual(RATING);
    const options = sent(send);
    expect(options.method).toBe("POST");
    expect(options.path).toBe(`/api/orders/${ID}/rating`);
    expect(options.body).toEqual({ stars: 4 });
  });

  it("listPendingRatings reads the caller's errands still to rate", async () => {
    const pending = {
      errandId: ID,
      rateeId: RATING.rateeId,
      raterRole: "courier",
      itemDescription: "1 × Iced latte",
      completedAt: SERVER_TIME,
    };
    const { send, api } = stubSend(ok({ items: [pending] }));
    await expect(api.listPendingRatings()).resolves.toEqual([pending]);
    expect(sent(send).path).toBe("/api/orders/ratings/pending");
  });

  it("getUserRating reads a user's rating, with or without an average", async () => {
    const { send, api } = stubSend(ok({ userId: RATING.rateeId, count: 2, average: 4.5 }));
    await expect(api.getUserRating(RATING.rateeId)).resolves.toEqual({
      userId: RATING.rateeId,
      count: 2,
      average: 4.5,
    });
    expect(sent(send).path).toBe(`/api/orders/users/${RATING.rateeId}/rating`);

    const none = stubSend(ok({ userId: RATING.rateeId, count: 0 }));
    await expect(none.api.getUserRating(RATING.rateeId)).resolves.toEqual({ userId: RATING.rateeId, count: 0 });
  });

  it("recordShown and recordSearch post only the contract's fields", async () => {
    const shown = stubSend(ok(null, 204));
    await shown.api.recordShown(
      [{ errandId: ID, position: 2, extra: true } as never],
      { ...FILTERS, sort: "nearest" } as never,
    );
    const shownOptions = sent(shown.send);
    expect(shownOptions.method).toBe("POST");
    expect(shownOptions.path).toBe("/api/orders/activity/shown");
    expect(shownOptions.body).toEqual({ items: [{ errandId: ID, position: 2 }], filters: FILTERS });

    const search = stubSend(ok(null, 204));
    await search.api.recordSearch(FILTERS);
    const searchOptions = sent(search.send);
    expect(searchOptions.path).toBe("/api/orders/activity/searches");
    expect(searchOptions.body).toEqual({ filters: FILTERS });
  });

  it.each([
    ["rate", (api: OrdersApi) => api.rate(ID, 4), { ...RATING, stars: "4" }],
    ["listPendingRatings", (api: OrdersApi) => api.listPendingRatings(), { items: [{ errandId: ID }] }],
    ["getUserRating", (api: OrdersApi) => api.getUserRating(ID), { userId: ID, count: "2" }],
  ])("%s refuses a body the spec does not promise", async (_name, call, body) => {
    const { api } = stubSend(ok(body));
    await expect(call(api)).rejects.toMatchObject({ status: 0 });
  });

  it("passes order-service's refusal through", async () => {
    const { api } = stubSend(fail(409, { error: "you have already rated this errand" }));
    await expect(api.rate(ID, 4)).rejects.toMatchObject({
      status: 409,
      message: "you have already rated this errand",
    });
  });
});

// AI-generated (edited by PENDING): D-094's suggestion on the open list.
describe("ordersApi listOpen suggestion", () => {
  const REC = { errandId: ID, confidence: 0.72, reason: "Suggested because it pays 10 credits." };

  it("keeps a recommendation the list carries", async () => {
    const { api } = stubSend(ok({ ...LIST, recommendation: REC }));
    await expect(api.listOpen()).resolves.toEqual({ ...LIST, recommendation: REC });
  });

  it("drops a recommendation the spec does not promise, keeping the list", async () => {
    const { api } = stubSend(ok({ ...LIST, recommendation: { errandId: ID, confidence: "high" } }));
    await expect(api.listOpen()).resolves.toEqual(LIST);
  });

  it("asks for none with recommend=false, and sends nothing extra otherwise", async () => {
    const off = stubSend(ok(LIST));
    await off.api.listOpen({ recommend: false });
    expect(sent(off.send).path).toBe("/api/orders/open?recommend=false");

    const on = stubSend(ok(LIST));
    await on.api.listOpen({ recommend: true });
    expect(sent(on.send).path).toBe("/api/orders/open");
  });
});
