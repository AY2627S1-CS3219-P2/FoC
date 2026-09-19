// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Shared transport plumbing, extracted from the supplier client so each
//   feature's api module does not repeat it.
// Author review: PENDING — <reviewer to complete>

/**
 * Transport only: URL joining, headers, JSON parsing, and the one failure mode
 * that is the same everywhere — the server not answering at all.
 *
 * DELIBERATELY NOT HERE: turning a non-2xx response into an error message.
 * supplier-service returns `{"error": "..."}`, but whether the other services
 * match is an interface decision owned by each service, and standardising one
 * envelope from the frontend would be making that decision for them
 * (frontend/AGENTS.md, Gotchas). Each feature's api module inspects `body` and
 * `status` itself and throws its own error type.
 */

/** Thrown when the request never reached the server. */
export class NetworkError extends Error {
  constructor(baseUrl: string) {
    super(`Could not reach the service at ${baseUrl}`);
    this.name = "NetworkError";
  }
}

/** A completed HTTP exchange, successful or not. */
export interface HttpResponse {
  ok: boolean;
  status: number;
  /** Parsed JSON body, or null when there was none or it was not JSON. */
  body: unknown;
}

export interface SendOptions {
  baseUrl: string;
  path: string;
  method?: "GET" | "POST" | "PUT" | "DELETE";
  /** Serialised as JSON; the Content-Type header is set for you. */
  body?: unknown;
  headers?: Record<string, string>;
}

export async function send({
  baseUrl,
  path,
  method = "GET",
  body,
  headers = {},
}: SendOptions): Promise<HttpResponse> {
  const requestHeaders = new Headers(headers);
  if (body !== undefined) requestHeaders.set("Content-Type", "application/json");

  let response: Response;
  try {
    response = await fetch(`${baseUrl}${path}`, {
      method,
      headers: requestHeaders,
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch {
    // fetch() rejects only on network failure. In development that is almost
    // always the service simply not running.
    throw new NetworkError(baseUrl);
  }

  if (response.status === 204) {
    return { ok: response.ok, status: response.status, body: null };
  }

  const parsed: unknown = await response.json().catch(() => null);
  return { ok: response.ok, status: response.status, body: parsed };
}

/** Builds a query string, omitting empty values. Returns "" or "?a=b". */
export function queryString(params: Record<string, string | undefined>): string {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value) search.set(key, value);
  }
  const query = search.toString();
  return query ? `?${query}` : "";
}
