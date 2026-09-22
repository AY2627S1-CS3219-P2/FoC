// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Ported the prototype's fetch calls in app.js into a single typed
//   client module for supplier-service, over the shared transport in lib/http.
//   2026-09-22: repointed from supplier-service's own port onto the API
//   Gateway (D-010, D-025b). Module-level functions became a factory taking
//   the authorized transport, because every call now carries a token.
// Author review: PENDING — <reviewer to complete>

import type { AuthorizedSend } from "../auth/session";
import { config } from "../../lib/config";
import { NetworkError, queryString } from "../../lib/http";
import type { Supplier, SupplierFilter, SupplierInput } from "./types";

/**
 * The only module that knows how to talk to supplier-service. frontend/AGENTS.md
 * requires one per backend service, with the same surface a generated OpenAPI
 * client would have, so swapping in the generated one later is a single-file
 * change. No component calls send() or fetch() directly.
 *
 * Every request goes to the **gateway**, never to supplier-service's own port
 * (D-010). The gateway proxies `/api/suppliers/*` onto supplier-service's own
 * `/suppliers` prefix, verifies the access token, strips any claim headers the
 * browser sent and injects its own (D-022, D-027).
 */

/** The gateway's public prefix for supplier-service (D-027). */
const PREFIX = "/api/suppliers";

/**
 * supplier-service's error shape: `{"error": "..."}` with a non-2xx status.
 * Decoded here rather than in lib/http because the other services have not
 * committed to this envelope — that is each owner's interface decision.
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
 * Builds the client over an authorized transport.
 *
 * A factory rather than module-level functions because the access token is
 * per-session state: threading it through a package-level variable would be
 * the global coupling root AGENTS.md §5 rules out. App.tsx constructs this
 * once and passes it down.
 *
 * ADMIN IS NO LONGER ASSERTED HERE. Until 2026-09-22 the write calls sent
 * `X-User-Role: ADMIN` themselves, which worked only because the browser
 * reached supplier-service directly. Through the gateway that header is
 * deleted on every route and replaced with the role from the verified token
 * (D-022) — so sending it is at best ignored, and reading this file should not
 * suggest a client can choose its own role. An admin action now succeeds
 * exactly when the logged-in account's `role` claim is ADMIN.
 */
export function createSuppliersApi(authorizedSend: AuthorizedSend): SuppliersApi {
  async function call<T>(
    path: string,
    init: { method?: "GET" | "POST" | "PUT" | "DELETE"; body?: unknown } = {},
  ): Promise<T> {
    // No guard on an empty base URL: under D-033 that is the normal value and
    // means "same origin", so a relative path is exactly right.
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
