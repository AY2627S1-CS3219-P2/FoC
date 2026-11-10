// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-09
// Scope: Client for credit-service's admin routes through the gateway (D-150 to
//   D-153): the flagged accounts, an account's wallet, Re-verify and the
//   credit half of the audit log. Hand-written from credit-service/api/openapi.yaml
//   on the same branch, like creditAccountsApi.ts.
// Author review: PENDING — <reviewer to complete>

import type { AuthorizedSend } from "../auth/session";
import { config } from "../../lib/config";
import { NetworkError, queryString, type HttpResponse } from "../../lib/http";
import { parseAuditPage, type AuditEntry, type AuditFilter, type Page } from "../admin/auditTypes";
import { auditQuery } from "../profile/usersApi";
import { CreditAccountsApiError } from "./creditAccountsApi";
import {
  parseAdminWallet,
  parseFlaggedPage,
  parseReverifyResult,
  type AdminWallet,
  type FlaggedAccount,
  type ReverifyResult,
} from "./creditAdminTypes";

/**
 * The gateway's public prefix for credit-service's admin routes (D-151), which
 * it forwards to `/api/v1/credits/admin` with the bearer token still attached.
 * credit-service answers 403 to any token that is not an admin's.
 */
const PREFIX = "/api/credits/admin";

/** The surface a generated OpenAPI client would expose for these routes. */
export interface CreditAdminApi {
  /** The flagged accounts, oldest flag first, 50 a page at most (D-104). */
  listFlaggedAccounts(limit: number, offset: number): Promise<Page<FlaggedAccount>>;
  /**
   * A user's account as stored, flagged or not, or null when they have none
   * (404 WALLET_NOT_READY). A flagged account's balances are not trusted.
   */
  getWallet(userId: string): Promise<AdminWallet | null>;
  /**
   * Re-runs the ledger checks on one account and clears its flag when they all
   * pass (D-150). A still-failing check is an ordinary answer (`STILL_FLAGGED`),
   * not an error.
   */
  reverify(userId: string): Promise<ReverifyResult>;
  /** The credit half of the admin audit log, newest first (D-152). */
  listAuditLog(filter?: AuditFilter): Promise<Page<AuditEntry>>;
}

/**
 * Builds the client over an authorized transport. App.tsx creates it once per
 * token store, alongside the other clients.
 */
export function createCreditAdminApi(authorizedSend: AuthorizedSend): CreditAdminApi {
  async function call(path: string, method: "GET" | "POST" = "GET"): Promise<HttpResponse> {
    try {
      return await authorizedSend({ baseUrl: config.gatewayBaseUrl, path, method });
    } catch (err) {
      if (err instanceof NetworkError) throw new CreditAccountsApiError(err.message, 0);
      throw err;
    }
  }

  async function ok(path: string, method: "GET" | "POST" = "GET"): Promise<unknown> {
    const response = await call(path, method);
    if (!response.ok) throw decodeError(response.body, response.status);
    return response.body;
  }

  return {
    async listFlaggedAccounts(limit, offset) {
      const page = parseFlaggedPage(await ok(`${PREFIX}/flagged-accounts${queryString({
        limit: String(limit),
        offset: offset ? String(offset) : undefined,
      })}`));
      if (!page) throw unusableResponse();
      return page;
    },

    async getWallet(userId) {
      const response = await call(`${PREFIX}/users/${encodeURIComponent(userId)}/account`);
      if (response.status === 404 && isWalletNotReady(response.body)) return null;
      if (!response.ok) throw decodeError(response.body, response.status);
      const wallet = parseAdminWallet(response.body);
      if (!wallet) throw unusableResponse();
      return wallet;
    },

    async reverify(userId) {
      const result = parseReverifyResult(await ok(`${PREFIX}/users/${encodeURIComponent(userId)}/reconcile`, "POST"));
      if (!result) throw unusableResponse();
      return result;
    },

    async listAuditLog(filter = {}) {
      const page = parseAuditPage(await ok(`${PREFIX}/audit-log${auditQuery(filter)}`), "credits", true);
      if (!page) throw unusableResponse();
      return page;
    },
  };
}

function isWalletNotReady(body: unknown): boolean {
  return typeof body === "object" && body !== null && (body as Record<string, unknown>)["code"] === "WALLET_NOT_READY";
}

/** credit-service sends `{code, message}`; the gateway's own errors send `{error}`. */
function decodeError(body: unknown, status: number): CreditAccountsApiError {
  let message = `Request failed (${status})`;
  if (typeof body === "object" && body !== null) {
    const record = body as Record<string, unknown>;
    if (typeof record["message"] === "string") message = record["message"];
    else if (typeof record["error"] === "string") message = record["error"];
  }
  return new CreditAccountsApiError(message, status);
}

/** A 2xx without the body the spec promises: nothing usable came back. */
function unusableResponse(): CreditAccountsApiError {
  return new CreditAccountsApiError("No usable response from credit-service", 0);
}
