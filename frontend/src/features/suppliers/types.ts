// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Transcribed supplier-service's JSON shapes into TypeScript types.
// Author review: Nigeltzy - Checked the simple generated converted file based on the team's architecture decisions made and requirements provided.

/**
 * Mirrors `supplierResponse` in
 * supplier-service/internal/httpapi/dto.go. That Go struct is the contract
 * until an OpenAPI spec exists (root AGENTS.md §8); when it does, this file is
 * replaced by generated types. Field names are snake_case because the wire
 * format is — do not "fix" them.
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

/** Mirrors `supplierRequest` — the body accepted by create and update. */
export type SupplierInput = Omit<
  Supplier,
  "id" | "created_at" | "updated_at"
>;

/** Query parameters accepted by `GET /suppliers`. */
export interface SupplierFilter {
  category?: string;
  search?: string;
}
