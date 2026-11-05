// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-11-04
// Scope: Browser error reporting: uncaught render errors, window errors and
//   unhandled rejections, each sent at once to the gateway's
//   POST /api/client-errors with the access token in memory.
// Author review: James Teo — reviewed and verified.

import { send } from "./http";

/**
 * Sends what nothing else caught to the gateway, which logs it with the
 * caller's user id, so a crash on someone's screen reaches the team's logs.
 *
 * - Only a signed-in page reports: nothing is sent while no access token is in
 *   memory. App shows the login and OTP screens only then, so they never
 *   report.
 * - Each report goes out at once over plain send(), never the authorized
 *   transport, so reporting never spends the refresh cookie. A refused or
 *   failed report is dropped: no queue, no retry, and no report about it.
 * - At most MAX_REPORTS per page load, and each kind and message only once,
 *   so an error thrown in a loop cannot flood the gateway.
 * - Nothing here throws or leaves a rejected promise: a reporter that failed
 *   would turn one error into two.
 */

/** What went uncaught: a render, a window "error" event, or a promise rejection. */
export type ReportKind = "render" | "error" | "rejection";

/** The body POST /api/client-errors takes. The gateway refuses any other field. */
export interface ClientErrorReport {
  kind: ReportKind;
  /** An Error's name and message, or only the typeof of anything else thrown. */
  message: string;
  stack: string;
  /** The screen the user was on. */
  view: string;
}

/** Sends one report with the given access token. */
export type PostReport = (report: ClientErrorReport, accessToken: string) => Promise<unknown>;

export interface ErrorReporterDeps {
  post: PostReport;
  /** Reads the access token in memory, or null when there is none. */
  getAccessToken: () => string | null;
  /** The current screen's name. */
  getView: () => string;
}

export interface ErrorReporter {
  /** For createRoot's onUncaughtError. React's component stack follows the error's own. */
  reportRender(error: unknown, componentStack: string | undefined): void;
  /**
   * A window "error" listener. An event without an Error is ignored, such as a
   * cross-origin script's "Script error." or a thrown string.
   */
  reportWindowError(event: Pick<ErrorEvent, "error">): void;
  /** A window "unhandledrejection" listener. */
  reportRejection(event: Pick<PromiseRejectionEvent, "reason">): void;
}

const MAX_REPORTS = 10;
const MAX_MESSAGE_CHARS = 500;
const MAX_STACK_BYTES = 2048;

/** The gateway answers this route itself, so no service client owns it. */
const CLIENT_ERRORS_PATH = "/api/client-errors";

/** Makes the page's reporter. Its count and the reports it has sent live in this closure. */
export function createErrorReporter({
  post,
  getAccessToken,
  getView,
}: ErrorReporterDeps): ErrorReporter {
  let sent = 0;
  const seen = new Set<string>();

  function report(kind: ReportKind, thrown: unknown, componentStack: string): void {
    if (sent >= MAX_REPORTS) return;
    const accessToken = getAccessToken();
    if (!accessToken) return;

    const { message, stack } = describe(thrown);
    const key = `${kind}:${message}`;
    if (seen.has(key)) return;

    const body: ClientErrorReport = {
      kind,
      message,
      stack: truncateUtf8(stack + componentStack, MAX_STACK_BYTES),
      view: getView(),
    };
    seen.add(key);
    sent += 1;
    post(body, accessToken).catch(() => {
      // The gateway could not be reached: dropped, never retried or reported.
    });
  }

  return {
    reportRender(error, componentStack) {
      safely(() => report("render", error, componentStack ?? ""));
    },
    reportWindowError(event) {
      safely(() => {
        if (event.error instanceof Error) report("error", event.error, "");
      });
    },
    reportRejection(event) {
      safely(() => report("rejection", event.reason, ""));
    },
  };
}

/**
 * Posts with plain send() and the token it is given: never the authorized
 * transport, which would refresh and retry after a 401.
 */
export function createGatewayPost(baseUrl: string): PostReport {
  return (report, accessToken) =>
    send({ baseUrl, path: CLIENT_ERRORS_PATH, method: "POST", body: report, accessToken });
}

/** A non-Error value is sent only as its typeof, since it could hold anything. */
function describe(thrown: unknown): { message: string; stack: string } {
  if (!(thrown instanceof Error)) return { message: typeof thrown, stack: "" };
  return {
    // String() puts the error's name before its message; Firefox and Safari
    // leave both out of the stack.
    message: String(thrown).slice(0, MAX_MESSAGE_CHARS),
    stack: typeof thrown.stack === "string" ? thrown.stack : "",
  };
}

/** The longest start of `text` that fits in `maxBytes` of UTF-8, never splitting a character. */
function truncateUtf8(text: string, maxBytes: number): string {
  const bytes = new TextEncoder().encode(text);
  if (bytes.length <= maxBytes) return text;
  let end = maxBytes;
  // A byte 10xxxxxx continues a character: back up to the byte that starts it.
  while (end > 0 && (bytes[end] & 0xc0) === 0x80) end -= 1;
  return new TextDecoder().decode(bytes.subarray(0, end));
}

/** Runs `fn` and drops anything it throws. */
function safely(fn: () => void): void {
  try {
    fn();
  } catch {
    // A failing dependency or a hostile error object: the report is dropped.
  }
}
