// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Shared transport plumbing, extracted from the supplier client so each
//   feature's api module does not repeat it.
// Author review: Nigeltzy - Used to create the original boilerplate generation, 
// seems like a valid typical implementation, with updates I instructed based on
// the contents of my teammates' system design decisions.

/**
 * Transport only: URL joining, headers, JSON parsing, and turning a request
 * that never reached the server into a NetworkError.
 *
 * A non-2xx response is returned, not thrown. Each feature's api module reads
 * `status` and `body` and throws its own error type.
 */

/** Thrown when the request never reached the server. */
export class NetworkError extends Error {
  constructor(baseUrl: string) {
    // A CORS refusal also rejects fetch(), indistinguishable from the host
    // being down, so the message does not say which it was.
    super(`No usable response from ${baseUrl || "the server"}`);
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
  // AI-generated (edited by nigeltzy).
  /**
   * Sent as `Authorization: Bearer <token>`. Null or omitted sends no
   * Authorization header, as for login. Most callers go through
   * features/auth/session, which supplies the token and retries once after a
   * refresh.
   */
  accessToken?: string | null;
}

export async function send({
  baseUrl,
  path,
  method = "GET",
  body,
  headers = {},
  accessToken = null,
}: SendOptions): Promise<HttpResponse> {
  const requestHeaders = new Headers(headers);
  if (body !== undefined) requestHeaders.set("Content-Type", "application/json");
  // AI-generated (edited by nigeltzy).
  if (accessToken) requestHeaders.set("Authorization", `Bearer ${accessToken}`);

  let response: Response;
  try {
    response = await fetch(`${baseUrl}${path}`, {
      method,
      headers: requestHeaders,
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch {
    // No response arrived: the host is down, or the browser refused it (CORS).
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
