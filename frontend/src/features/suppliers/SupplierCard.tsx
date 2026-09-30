// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Ported supplierCardHtml() from the prototype to a React component.
// Author review: Nigeltzy - Checked the simple generated converted file.

import type { Supplier } from "./types";
import { SupplierImage } from "./SupplierImage";

interface SupplierCardProps {
  supplier: Supplier;
  onSelect: (supplier: Supplier) => void;
}

export function SupplierCard({ supplier, onSelect }: SupplierCardProps) {
  return (
    <button className="supplier-card" onClick={() => onSelect(supplier)}>
      <SupplierImage
        className="supplier-card-img"
        src={supplier.image_url}
        alt={supplier.name}
      />
      <div className="supplier-card-body">
        <span className="badge badge-type">{supplier.type}</span>
        <span className="supplier-card-name">{supplier.name}</span>
        <span className="supplier-card-meta">
          {supplier.building}
          {supplier.floor ? ` · Floor ${supplier.floor}` : ""}
        </span>
        <span className="supplier-card-meta">
          {supplier.location_description}
        </span>
        <span
          className={`badge ${
            supplier.is_available ? "badge-available" : "badge-unavailable"
          }`}
        >
          {supplier.is_available ? "Available" : "Unavailable"}
        </span>
      </div>
    </button>
  );
}
