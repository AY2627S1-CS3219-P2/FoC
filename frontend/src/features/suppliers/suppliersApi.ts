// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Ported the prototype's fetch calls in app.js into a single typed
//   client module for supplier-service, over the shared transport in lib/http.
//   2026-09-22: repointed from supplier-service's own port onto the API
//   Gateway (D-010, D-025b). Module-level functions became a factory taking
//   the authorized transport, because every call now carries a token.
// Author review: Nigeltzy - The AI is used to generate boilerplate simple connections
// to the endpoints of my teammate's service. The code seems valid and appropriate based
// on what was supplied to them.

import type { AuthorizedSend } from "../auth/session";
import { config } from "../../lib/config";
import { NetworkError, queryString } from "../../lib/http";
import type { Supplier, SupplierFilter, SupplierInput } from "./types";

/**
 * The only module that talks to supplier-service, with the surface a
 * generated OpenAPI client would have, so swapping one in is a one-file
 * change. Components never call send() or fetch() themselves. Every request
 * goes to the gateway under PREFIX, never to supplier-service's own port.
 */

/** The gateway's public prefix for supplier-service. */
const PREFIX = "/api/suppliers";

/**
 * An error from a supplier call. supplier-service answers `{"error": "..."}`
 * with a non-2xx status; that is decoded here, not in lib/http, because the
 * other services have not settled on the same envelope. `status` is 0 when
 * no response arrived.
 */
export class SupplierApiError extends Error {
  readonly status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = "SupplierApiError";
    this.status = status;
  }
}

function decodeError(body: unknown, status: number): SupplierApiError {
  const message =
    body && typeof body === "object" && "error" in body
      ? String((body as { error: unknown }).error)
      : `Request failed (${status})`;
  return new SupplierApiError(message, status);
}

/** The surface a generated OpenAPI client would expose. */
export interface SuppliersApi {
  listSuppliers(filter?: SupplierFilter): Promise<Supplier[]>;
  getSupplier(id: string): Promise<Supplier>;
  createSupplier(input: SupplierInput): Promise<Supplier>;
  updateSupplier(id: string, input: SupplierInput): Promise<Supplier>;
  deleteSupplier(id: string): Promise<void>;
}

/**
 * Builds the client over an authorized transport. App.tsx creates it once per
 * token store and passes it down.
 *
 * No call sends a role header: an admin write succeeds only when the signed-in
 * account's access token carries the ADMIN role.
 */
export function createSuppliersApi(authorizedSend: AuthorizedSend): SuppliersApi {
  async function call<T>(
    path: string,
    init: { method?: "GET" | "POST" | "PUT" | "DELETE"; body?: unknown } = {},
  ): Promise<T> {
    // An empty base URL is the default and makes `path` a same-origin
    // relative URL, so there is no guard for it.
    let response;
    try {
      response = await authorizedSend({
        baseUrl: config.gatewayBaseUrl,
        path,
        ...init,
      });
    } catch (err) {
      if (err instanceof NetworkError) {
        throw new SupplierApiError(err.message, 0);
      }
      throw err;
    }
    if (!response.ok) throw decodeError(response.body, response.status);
    return response.body as T;
  }

  return {
    listSuppliers(filter: SupplierFilter = {}) {
      const query = queryString({ q: filter.search, category: filter.category });
      return call<Supplier[]>(`${PREFIX}${query}`);
    },

    getSupplier(id: string) {
      return call<Supplier>(`${PREFIX}/${encodeURIComponent(id)}`);
    },

    createSupplier(input: SupplierInput) {
      return call<Supplier>(PREFIX, { method: "POST", body: input });
    },

    updateSupplier(id: string, input: SupplierInput) {
      return call<Supplier>(`${PREFIX}/${encodeURIComponent(id)}`, {
        method: "PUT",
        body: input,
      });
    },

    deleteSupplier(id: string) {
      return call<void>(`${PREFIX}/${encodeURIComponent(id)}`, {
        method: "DELETE",
      });
    },
  };
}
