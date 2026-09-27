// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Transcribed supplier-service's JSON shapes into TypeScript types.
// Author review: Nigeltzy - Checked the simple generated converted file based on the team's architecture decisions made and requirements provided.

/**
 * A supplier as supplier-service returns it through the gateway. Field names
 * are snake_case because the JSON is; generated types replace this file once
 * supplier-service has an OpenAPI spec.
 */
export interface Supplier {
  id: string;
  name: string;
  type: string;
  building: string;
  floor: string;
  location_description: string;
  latitude: number;
  longitude: number;
  opening_time: string;
  closing_time: string;
  image_url: string;
  description: string;
  is_available: boolean;
  created_at: string;
  updated_at: string;
}

/** The body sent to create and update: a Supplier without its server-set fields. */
export type SupplierInput = Omit<
  Supplier,
  "id" | "created_at" | "updated_at"
>;

/** Filters for listSuppliers; `search` is sent as the `q` query parameter. */
export interface SupplierFilter {
  category?: string;
  search?: string;
}
