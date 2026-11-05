// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-04
// Scope: Tests for the browser error reporter: what a report carries, when
//   nothing is sent, the per-load cap and de-duplication, truncation, the
//   gateway request, and that no failure ever escapes the reporter.
// Author review: James Teo — reviewed and verified.

import { afterEach, describe, expect, it, vi } from "vitest";
import {
  createErrorReporter,
  createGatewayPost,
  type ClientErrorReport,
  type ErrorReporter,
  type ErrorReporterDeps,
  type PostReport,
  type ReportKind,
} from "./errorReporter";

const TOKEN = "access-token";

interface Sent {
  report: ClientErrorReport;
  accessToken: string;
}

/**
 * A reporter signed in on Profile whose post records every call and answers
 * 204. `deps` replaces any part; a replacement post is recorded too.
 *
 * The post is a plain function, not vi.fn(): a vi.fn() handles the promise
 * its implementation returns, which would hide a rejection the reporter left
 * unhandled.
 */
function reporterWith(deps: Partial<ErrorReporterDeps> = {}) {
  const answer: PostReport = deps.post ?? (async () => ({ ok: true, status: 204, body: null }));
  const sent: Sent[] = [];
  const reporter = createErrorReporter({
    getAccessToken: () => TOKEN,
    getView: () => "profile",
    ...deps,
    post: (report, accessToken) => {
      sent.push({ report, accessToken });
      return answer(report, accessToken);
    },
  });
  return { reporter, sent };
}

function reportsIn(sent: Sent[]): ClientErrorReport[] {
  return sent.map(({ report }) => report);
}

/** An error with a fixed stack, so no test depends on where it ran. */
function withStack<E extends Error>(error: E, stack: string): E {
  error.stack = stack;
  return error;
}

/** Each way an error reaches the reporter, keyed by the kind it is sent as. */
const RAISE: Record<ReportKind, (reporter: ErrorReporter, thrown: Error) => void> = {
  render: (reporter, thrown) => reporter.reportRender(thrown, undefined),
  error: (reporter, thrown) => reporter.reportWindowError({ error: thrown }),
  rejection: (reporter, thrown) => reporter.reportRejection({ reason: thrown }),
};
const KINDS = Object.keys(RAISE) as ReportKind[];

/** The part of Node's process these tests use; tsconfig.app.json has no Node types. */
interface NodeProcess {
  on(event: "unhandledRejection", listener: (reason: unknown) => void): void;
  off(event: "unhandledRejection", listener: (reason: unknown) => void): void;
}

/**
 * Runs `act`, then lets one macrotask pass: Node raises a rejection nobody
 * handled once the microtasks after `act` have drained. Returns those.
 */
async function rejectionsLeftBy(act: () => void): Promise<unknown[]> {
  const { process } = globalThis as unknown as { process: NodeProcess };
  const left: unknown[] = [];
  const listener = (reason: unknown) => {
    left.push(reason);
  };
  process.on("unhandledRejection", listener);
  try {
    act();
    await new Promise((resolve) => setTimeout(resolve, 0));
  } finally {
    process.off("unhandledRejection", listener);
  }
  return left;
}

describe("createErrorReporter", () => {
  it("sends kind, message, stack and view with the access token", () => {
    const { reporter, sent } = reporterWith();
    const stack = "TypeError: x is undefined\n    at ProfileView (ProfileView.tsx:3:7)";
    // Detached, as addEventListener calls it.
    const { reportWindowError } = reporter;

    reportWindowError({ error: withStack(new TypeError("x is undefined"), stack) });

    expect(sent).toStrictEqual([
      {
        report: { kind: "error", message: "TypeError: x is undefined", stack, view: "profile" },
        accessToken: TOKEN,
      },
    ]);
  });

  it.each(KINDS)("sends a %s report under that kind", (kind) => {
    const { reporter, sent } = reporterWith();
    RAISE[kind](reporter, new Error("boom"));
    expect(reportsIn(sent).map((report) => report.kind)).toEqual([kind]);
  });

  it("puts React's component stack after a render error's own stack", () => {
    const { reporter, sent } = reporterWith();
    const ownStack = "Error: boom\n    at CreditsView (CreditsView.tsx:9:9)";
    const componentStack = "\n    at CreditsView\n    at App";

    reporter.reportRender(withStack(new Error("boom"), ownStack), componentStack);

    expect(reportsIn(sent)).toStrictEqual([
      { kind: "render", message: "Error: boom", stack: ownStack + componentStack, view: "profile" },
    ]);
  });

  it("reads the token and the view at each report, not once", () => {
    let token: string | null = "first-token";
    let view = "home";
    const { reporter, sent } = reporterWith({
      getAccessToken: () => token,
      getView: () => view,
    });

    reporter.reportRejection({ reason: new Error("one") });
    token = "second-token";
    view = "credits";
    reporter.reportRejection({ reason: new Error("two") });

    expect(sent.map(({ report, accessToken }) => [report.view, accessToken])).toEqual([
      ["home", "first-token"],
      ["credits", "second-token"],
    ]);
  });

  it("sends nothing while no access token is in memory, and does not count it", () => {
    let token: string | null = null;
    const { reporter, sent } = reporterWith({ getAccessToken: () => token });

    for (const kind of KINDS) RAISE[kind](reporter, new Error("signed out"));
    expect(sent).toHaveLength(0);

    // Held back, not spent: once signed in, the same error is sent.
    token = TOKEN;
    RAISE.error(reporter, new Error("signed out"));
    expect(sent).toHaveLength(1);
  });

  it("sends at most 10 reports per page load", () => {
    const { reporter, sent } = reporterWith();

    for (let i = 1; i <= 12; i += 1) reporter.reportRejection({ reason: new Error(`error ${i}`) });

    expect(sent).toHaveLength(10);
    expect(reportsIn(sent).at(-1)?.message).toBe("Error: error 10");
  });

  it("sends each kind and message only once", () => {
    const { reporter, sent } = reporterWith();

    for (const kind of KINDS) {
      RAISE[kind](reporter, new Error("same"));
      RAISE[kind](reporter, new Error("same"));
    }
    reporter.reportRejection({ reason: new Error("different") });

    expect(reportsIn(sent).map(({ kind, message }) => `${kind} ${message}`)).toEqual([
      "render Error: same",
      "error Error: same",
      "rejection Error: same",
      "rejection Error: different",
    ]);
  });

  it("keeps its count and what it has sent to itself", () => {
    const first = reporterWith();
    const second = reporterWith();

    for (let i = 1; i <= 10; i += 1) {
      first.reporter.reportRejection({ reason: new Error(`error ${i}`) });
    }
    second.reporter.reportRejection({ reason: new Error("error 1") });

    expect(first.sent).toHaveLength(10);
    expect(second.sent).toHaveLength(1);
  });

  it("cuts the message to 500 characters", () => {
    const { reporter, sent } = reporterWith();

    reporter.reportRejection({ reason: new Error("x".repeat(600)) });

    const [report] = reportsIn(sent);
    expect(report.message).toHaveLength(500);
    expect(report.message).toBe(`Error: ${"x".repeat(600)}`.slice(0, 500));
  });

  it.each([
    ["one-byte characters", "a".repeat(5000), "a".repeat(2048)],
    ["two-byte characters", "é".repeat(2000), "é".repeat(1024)],
    ["a two-byte character across the limit", `a${"é".repeat(2000)}`, `a${"é".repeat(1023)}`],
    [
      "a four-byte character across the limit",
      `ab${"😀".repeat(1000)}`,
      `ab${"😀".repeat(511)}`,
    ],
  ])("cuts the stack to 2 KB of UTF-8 with %s, never splitting one", (_label, stack, expected) => {
    const { reporter, sent } = reporterWith();

    reporter.reportRejection({ reason: withStack(new Error("boom"), stack) });

    const [report] = reportsIn(sent);
    expect(report.stack).toBe(expected);
    expect(new TextEncoder().encode(report.stack).length).toBeLessThanOrEqual(2048);
  });

  it("keeps a render error's stack and component stack within 2 KB together", () => {
    const { reporter, sent } = reporterWith();
    const ownStack = `Error: boom\n${"    at Frame (frame.tsx:1:1)\n".repeat(50)}`;
    const componentStack = "\n    at CreditsView\n    at App".repeat(50);

    reporter.reportRender(withStack(new Error("boom"), ownStack), componentStack);

    expect(reportsIn(sent)[0].stack).toBe((ownStack + componentStack).slice(0, 2048));
  });

  it.each([
    ["a string", "the user's search text", "string"],
    ["a number", 42, "number"],
    ["an object", { token: "a secret" }, "object"],
    ["null", null, "object"],
    ["undefined", undefined, "undefined"],
  ])("sends only the typeof of a rejection with %s", (_label, reason, expected) => {
    const { reporter, sent } = reporterWith();

    reporter.reportRejection({ reason });

    expect(reportsIn(sent)).toStrictEqual([
      { kind: "rejection", message: expected, stack: "", view: "profile" },
    ]);
  });

  it("sends only the typeof and the component stack of a render that threw a non-Error", () => {
    const { reporter, sent } = reporterWith();

    reporter.reportRender("the user's search text", "\n    at App");

    expect(reportsIn(sent)).toStrictEqual([
      { kind: "render", message: "string", stack: "\n    at App", view: "profile" },
    ]);
  });

  it.each([
    ["no error, as a cross-origin script's has", null],
    ["a thrown string", "boom"],
    ["an object shaped like an Error", { name: "Error", message: "boom", stack: "at x" }],
  ])("ignores an error event carrying %s", (_label, error) => {
    const { reporter, sent } = reporterWith();
    const { reportWindowError } = reporter;

    reportWindowError({ error });

    expect(sent).toHaveLength(0);
  });

  it.each<[string, PostReport]>([
    ["refuses it", async () => ({ ok: false, status: 500, body: null })],
    ["cannot be reached", () => Promise.reject(new Error("offline"))],
  ])("drops a report when the gateway %s and never sends it again", async (_label, answer) => {
    const { reporter, sent } = reporterWith({ post: answer });

    const left = await rejectionsLeftBy(() => {
      reporter.reportRejection({ reason: new Error("boom") });
    });
    reporter.reportRejection({ reason: new Error("boom") });

    expect(left).toEqual([]);
    expect(sent).toHaveLength(1);
  });

  const fail = (): never => {
    throw new Error("dependency failed");
  };

  it.each<[string, Partial<ErrorReporterDeps>]>([
    ["the token getter throws", { getAccessToken: fail }],
    ["the view getter throws", { getView: fail }],
    ["the post throws", { post: fail }],
    ["the post rejects", { post: () => Promise.reject(new Error("offline")) }],
  ])("lets nothing escape when %s", async (_label, deps) => {
    const { reporter } = reporterWith(deps);

    const left = await rejectionsLeftBy(() => {
      for (const kind of KINDS) {
        expect(() => RAISE[kind](reporter, new Error(`${kind} failed`))).not.toThrow();
      }
    });

    expect(left).toEqual([]);
  });

  it("does not count a report it could not finish", () => {
    let viewFails = true;
    const { reporter, sent } = reporterWith({
      getView: () => {
        if (viewFails) throw new Error("no view");
        return "home";
      },
    });

    reporter.reportRejection({ reason: new Error("boom") });
    viewFails = false;
    reporter.reportRejection({ reason: new Error("boom") });

    expect(reportsIn(sent).map(({ message, view }) => `${message} on ${view}`)).toEqual([
      "Error: boom on home",
    ]);
  });

  it("lets nothing escape from an error whose message cannot be read", () => {
    const { reporter, sent } = reporterWith();
    const hostile = new Error("unreadable");
    Object.defineProperty(hostile, "message", {
      get() {
        throw new Error("no message for you");
      },
    });

    for (const kind of KINDS) {
      expect(() => RAISE[kind](reporter, hostile)).not.toThrow();
    }
    expect(sent).toHaveLength(0);
  });
});

describe("createGatewayPost", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("POSTs exactly the report to /api/client-errors with the access token", async () => {
    const fetchMock = vi.fn(
      async (_url: string, _init?: RequestInit) => new Response(null, { status: 204 }),
    );
    vi.stubGlobal("fetch", fetchMock);
    const report: ClientErrorReport = {
      kind: "rejection",
      message: "Error: offline",
      stack: "Error: offline\n    at load (load.ts:1:1)",
      view: "credits",
    };

    await createGatewayPost("")(report, TOKEN);

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe("/api/client-errors");
    expect(init?.method).toBe("POST");
    const headers = new Headers(init?.headers);
    expect(headers.get("Authorization")).toBe(`Bearer ${TOKEN}`);
    // The gateway mints request ids; the browser sends none.
    expect(headers.get("X-Request-Id")).toBeNull();
    expect(JSON.parse(String(init?.body))).toStrictEqual(report);
  });

  it.each<[string, () => Response]>([
    ["204", () => new Response(null, { status: 204 })],
    ["401", () => new Response(null, { status: 401 })],
    ["500", () => new Response(null, { status: 500 })],
    [
      "nothing, the request failing",
      () => {
        throw new TypeError("fetch failed");
      },
    ],
  ])("sends one request, and no refresh, when the gateway answers %s", async (_label, answer) => {
    const fetchMock = vi.fn(async (_url: string, _init?: RequestInit) => answer());
    vi.stubGlobal("fetch", fetchMock);
    const reporter = createErrorReporter({
      post: createGatewayPost(""),
      getAccessToken: () => TOKEN,
      getView: () => "home",
    });

    const left = await rejectionsLeftBy(() => {
      reporter.reportRejection({ reason: new Error("boom") });
    });

    expect(left).toEqual([]);
    expect(fetchMock.mock.calls.map(([url]) => url)).toEqual(["/api/client-errors"]);
  });
});
