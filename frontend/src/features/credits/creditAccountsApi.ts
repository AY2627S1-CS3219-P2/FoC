// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-18
// Scope: Client for credit-service's account routes through the gateway — the
//   signed-in user's balance and transaction history — hand-written by
//   transcribing credit-service/api/openapi.yaml: first from main at 27da39b,
//   then on 2026-10-21 from main at c914fe5 (#44), with cursor paging and the
//   {code, message} error body. Includes how a "no account yet" 404 is told
//   apart from a route that is not there. Replaces the mock creditsApi.ts.
// Author review: James Teo — reviewed and verified.

import type { AuthorizedSend } from "../auth/session";
import { config } from "../../lib/config";
import { NetworkError, queryString, type HttpResponse } from "../../lib/http";
import type { AccountResponse, TransactionOperation, TransactionListResponse } from "./creditTypes";

/**
 * The only module that calls credit-service. Named for the gateway route
 * `/api/credits/accounts`, the only part of credit-service the gateway lets
 * the browser reach; the gateway forwards it to `/api/v1/credits/accounts`
 * with the bearer token still attached.
 *
 * Hand-written rather than generated, the same way usersApi.ts is written
 * against user-service's spec. When credit-service's spec changes, this file
 * and creditTypes.ts are re-checked against it.
 */

/** The gateway's public prefix for credit-service's account routes. */
const PREFIX = "/api/credits/accounts";

/**
 * An error from a credit-service call, keeping its message when there is one. `status` is 0 when no usable response arrived. A screen
 * shows any of these as "credits are unavailable right now".
 */
export class CreditAccountsApiError extends Error {
  readonly status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = "CreditAccountsApiError";
    this.status = status;
  }
}

/** The surface a generated OpenAPI client would expose for these routes. */
export interface CreditAccountsApi {
  /**
   * The signed-in user's balance, or null when credit-service answers that
   * they have no account yet. A student's account opens a few seconds after
   * their email is verified, so null is an ordinary answer, not a failure.
   * An account credit-service reports as TEMPORARILY_UNAVAILABLE throws.
   */
  getAccount(): Promise<AccountResponse | null>;
  /**
   * One page of the signed-in user's transactions, newest first: the first
   * page without `cursor`, then the page after the one whose `nextCursor` is
   * passed. A user with no account gets an empty page. `limit` is 1–100.
   */
  listTransactions(limit: number, cursor?: string): Promise<TransactionListResponse>;
}

/**
 * Builds the client over an authorized transport. App.tsx creates it once per
 * token store, alongside the other clients.
 */
export function createCreditAccountsApi(authorizedSend: AuthorizedSend): CreditAccountsApi {
  async function call(path: string): Promise<HttpResponse> {
    try {
      return await authorizedSend({ baseUrl: config.gatewayBaseUrl, path });
    } catch (err) {
      if (err instanceof NetworkError) throw new CreditAccountsApiError(err.message, 0);
      throw err;
    }
  }

  return {
    async getAccount() {
      const response = await call(`${PREFIX}/me`);
      if (isNoAccount(response)) return null;
      if (!response.ok) throw decodeError(response.body, response.status);
      if (!isAccount(response.body)) throw unusableResponse();
      return response.body;
    },

    async listTransactions(limit, cursor) {
      const query = queryString({ limit: String(limit), cursor });
      const response = await call(`${PREFIX}/me/transactions${query}`);
      // The spec names no 404 here; this assumes a user with no account is
      // answered the way accounts/me answers them.
      if (isNoAccount(response)) return { items: [], nextCursor: null };
      if (!response.ok) throw decodeError(response.body, response.status);
      if (!isTransactionPage(response.body)) throw unusableResponse();
      return response.body;
    },
  };
}

/**
 * True for credit-service's "no account" answer: a 404 with the spec's error
 * code WALLET_NOT_READY. Any other 404, such as the plain-text 404 of a
 * service that does not serve the route yet, is an unavailable service, and
 * throws.
 */
function isNoAccount(response: HttpResponse): boolean {
  return (
    response.status === 404 &&
    isRecord(response.body) &&
    response.body["code"] === "WALLET_NOT_READY"
  );
}

/** credit-service sends `{code, message}`; the gateway's own errors send `{error}`. */
function decodeError(body: unknown, status: number): CreditAccountsApiError {
  let message = `Request failed (${status})`;
  if (isRecord(body) && typeof body["message"] === "string") message = body["message"];
  else if (isRecord(body) && typeof body["error"] === "string") message = body["error"];
  return new CreditAccountsApiError(message, status);
}

/** A 2xx without the body the spec promises: nothing usable came back. */
function unusableResponse(): CreditAccountsApiError {
  return new CreditAccountsApiError("No usable response from credit-service", 0);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/** Only a READY account carries its figures. */
function isAccount(value: unknown): value is AccountResponse {
  return (
    isRecord(value) &&
    value["creditStatus"] === "READY" &&
    typeof value["availableCredits"] === "number" &&
    typeof value["reservedCredits"] === "number"
  );
}

/** Checks what a screen reads without a guard: each row's id and deltas, and the cursor. */
function isTransactionPage(value: unknown): value is TransactionListResponse {
  const cursor = isRecord(value) ? value["nextCursor"] : undefined;
  return (
    isRecord(value) &&
    Array.isArray(value["items"]) &&
    value["items"].every(isTransaction) &&
    (cursor === undefined || cursor === null || typeof cursor === "string")
  );
}

function isTransaction(value: unknown): value is TransactionOperation {
  return (
    isRecord(value) &&
    typeof value["operationId"] === "string" &&
    typeof value["availableDelta"] === "number" &&
    typeof value["reservedDelta"] === "number"
  );
}
