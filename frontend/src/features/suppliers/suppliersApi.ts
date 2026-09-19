// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Ported the prototype's fetch calls in app.js into a single typed
//   client module for supplier-service, over the shared transport in lib/http.
// Author review: PENDING — <reviewer to complete>

import { config } from "../../lib/config";
import { NetworkError, queryString, send } from "../../lib/http";
import type { Supplier, SupplierFilter, SupplierInput } from "./types";

/**
 * The only module that knows how to talk to supplier-service. frontend/AGENTS.md
 * requires one per backend service, with the same surface a generated OpenAPI
 * client would have, so swapping in the generated one later is a single-file
 * change. No component calls send() or fetch() directly.
 */

const BASE_URL = config.supplierBaseUrl;

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

/**
 * Interim admin auth: supplier-service trusts an `X-User-Role: ADMIN` header
 * verbatim (its internal/middleware/auth.go). A development stand-in, not
 * access control — see the Gotchas in frontend/AGENTS.md.
 */
const ADMIN_HEADERS = { "X-User-Role": "ADMIN" };

async function call<T>(
  path: string,
  init: Omit<Parameters<typeof send>[0], "baseUrl" | "path"> = {},
): Promise<T> {
  let response;
  try {
    response = await send({ baseUrl: BASE_URL, path, ...init });
  } catch (err) {
    if (err instanceof NetworkError) {
      throw new SupplierApiError(err.message, 0);
    }
    throw err;
  }
  if (!response.ok) throw decodeError(response.body, response.status);
  return response.body as T;
}

export function listSuppliers(filter: SupplierFilter = {}): Promise<Supplier[]> {
  const query = queryString({ q: filter.search, category: filter.category });
  return call<Supplier[]>(`/suppliers${query}`);
}

export function getSupplier(id: string): Promise<Supplier> {
  return call<Supplier>(`/suppliers/${encodeURIComponent(id)}`);
}

export function createSupplier(input: SupplierInput): Promise<Supplier> {
  return call<Supplier>("/suppliers", {
    method: "POST",
    body: input,
    headers: ADMIN_HEADERS,
  });
}

export function updateSupplier(
  id: string,
  input: SupplierInput,
): Promise<Supplier> {
  return call<Supplier>(`/suppliers/${encodeURIComponent(id)}`, {
    method: "PUT",
    body: input,
    headers: ADMIN_HEADERS,
  });
}

export function deleteSupplier(id: string): Promise<void> {
  return call<void>(`/suppliers/${encodeURIComponent(id)}`, {
    method: "DELETE",
    headers: ADMIN_HEADERS,
  });
}
