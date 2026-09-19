// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Ported openDetail() from the prototype, replacing window.confirm()
//   with an inline confirmation step.
// Author review: PENDING — <reviewer to complete>

import { useState } from "react";
import type { Supplier } from "./types";
import { SupplierImage } from "./SupplierImage";

interface SupplierDetailProps {
  supplier: Supplier;
  isAdmin: boolean;
  onEdit: (supplier: Supplier) => void;
  onDelete: (supplier: Supplier) => void;
}

export function SupplierDetail({
  supplier,
  isAdmin,
  onEdit,
  onDelete,
}: SupplierDetailProps) {
  const [confirmingDelete, setConfirmingDelete] = useState(false);

  return (
    <>
      <SupplierImage
        className="detail-img"
        src={supplier.image_url}
        alt={supplier.name}
      />
      <h2 className="detail-title">{supplier.name}</h2>
      <span className="badge badge-type">{supplier.type}</span>

      <div className="detail-row">
        <strong>Location:</strong> {supplier.building}
        {supplier.floor ? `, Floor ${supplier.floor}` : ""} —{" "}
        {supplier.location_description}
      </div>
      <div className="detail-row">
        <strong>Hours:</strong> {supplier.opening_time || "–"} to{" "}
        {supplier.closing_time || "–"}
      </div>
      <div className="detail-row">
        <strong>Coordinates:</strong> {supplier.latitude}, {supplier.longitude}
      </div>
      {supplier.description && (
        <div className="detail-row">
          <strong>Description:</strong> {supplier.description}
        </div>
      )}
      <div className="detail-row">
        <strong>Status:</strong>{" "}
        <span
          className={`badge ${
            supplier.is_available ? "badge-available" : "badge-unavailable"
          }`}
        >
          {supplier.is_available ? "Available" : "Unavailable"}
        </span>
      </div>

      {isAdmin && !confirmingDelete && (
        <div className="modal-actions">
          <button className="btn btn-secondary" onClick={() => onEdit(supplier)}>
            Edit
          </button>
          <button
            className="btn btn-danger"
            onClick={() => setConfirmingDelete(true)}
          >
            Delete
          </button>
        </div>
      )}

      {isAdmin && confirmingDelete && (
        <div className="confirm-box">
          <p>
            Delete <strong>{supplier.name}</strong>? This soft-deletes the
            record — it is marked unavailable, not removed.
          </p>
          <div className="modal-actions">
            <button
              className="btn btn-danger"
              onClick={() => onDelete(supplier)}
            >
              Yes, delete
            </button>
            <button
              className="btn btn-secondary"
              onClick={() => setConfirmingDelete(false)}
            >
              Cancel
            </button>
          </div>
        </div>
      )}
    </>
  );
}
