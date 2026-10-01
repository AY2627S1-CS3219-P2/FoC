// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Ported openForm()/submitForm() from the prototype to a controlled
//   React form.
// Author review: Nigeltzy - Checked the simple generated converted file based on the prototype and requirements provided.
//
// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5), date: 2026-10-01
// Scope: Renamed the form-row/form-row-grid classes to the field/field-grid
//   classes every other form in this app already uses (login, register,
//   new errand). form-row/form-row-grid had no matching CSS at all, so the
//   form fell back to unstyled inline browser defaults — fine by accident
//   at desktop width, visibly broken (cramped, overlapping) at phone width.
// Author review: PENDING — <reviewer to complete>

import { useState, type FormEvent } from "react";
import type { Supplier, SupplierInput } from "./types";

interface SupplierFormProps {
  /** Undefined when creating; the existing record when editing. */
  existing?: Supplier;
  submitting: boolean;
  onSubmit: (input: SupplierInput) => void;
  onCancel: () => void;
}

const EMPTY: SupplierInput = {
  name: "",
  type: "",
  building: "",
  floor: "",
  location_description: "",
  latitude: 0,
  longitude: 0,
  opening_time: "",
  closing_time: "",
  image_url: "",
  description: "",
  is_available: true,
};

/**
 * Client-side checks are UX only: required fields, and number inputs for the
 * coordinates. supplier-service's rejections are shown as a toast.
 */
export function SupplierForm({
  existing,
  submitting,
  onSubmit,
  onCancel,
}: SupplierFormProps) {
  const [form, setForm] = useState<SupplierInput>(() =>
    existing ? toInput(existing) : EMPTY,
  );

  const set = <K extends keyof SupplierInput>(
    key: K,
    value: SupplierInput[K],
  ) => setForm((prev) => ({ ...prev, [key]: value }));

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault();
    onSubmit(form);
  };

  return (
    <>
      <h2 className="detail-title">
        {existing ? "Edit Supplier" : "Add Supplier"}
      </h2>
      <form onSubmit={handleSubmit}>
        <div className="field">
          <label htmlFor="f-name">Name</label>
          <input
            id="f-name"
            required
            value={form.name}
            onChange={(e) => set("name", e.target.value)}
          />
        </div>

        <div className="field-grid">
          <div className="field">
            <label htmlFor="f-type">Type / category</label>
            <input
              id="f-type"
              required
              value={form.type}
              onChange={(e) => set("type", e.target.value)}
            />
          </div>
          <div className="field">
            <label htmlFor="f-building">Building</label>
            <input
              id="f-building"
              required
              value={form.building}
              onChange={(e) => set("building", e.target.value)}
            />
          </div>
        </div>

        <div className="field-grid">
          <div className="field">
            <label htmlFor="f-floor">Floor</label>
            <input
              id="f-floor"
              value={form.floor}
              onChange={(e) => set("floor", e.target.value)}
            />
          </div>
          <div className="field">
            <label htmlFor="f-loc">Location description</label>
            <input
              id="f-loc"
              required
              value={form.location_description}
              onChange={(e) => set("location_description", e.target.value)}
            />
          </div>
        </div>

        <div className="field-grid">
          <div className="field">
            <label htmlFor="f-lat">Latitude</label>
            <input
              id="f-lat"
              type="number"
              step="any"
              required
              value={form.latitude}
              onChange={(e) => set("latitude", Number(e.target.value))}
            />
          </div>
          <div className="field">
            <label htmlFor="f-lng">Longitude</label>
            <input
              id="f-lng"
              type="number"
              step="any"
              required
              value={form.longitude}
              onChange={(e) => set("longitude", Number(e.target.value))}
            />
          </div>
        </div>

        <div className="field-grid">
          <div className="field">
            <label htmlFor="f-open">Opening time (HH:MM)</label>
            <input
              id="f-open"
              placeholder="09:00"
              value={form.opening_time}
              onChange={(e) => set("opening_time", e.target.value)}
            />
          </div>
          <div className="field">
            <label htmlFor="f-close">Closing time (HH:MM)</label>
            <input
              id="f-close"
              placeholder="18:00"
              value={form.closing_time}
              onChange={(e) => set("closing_time", e.target.value)}
            />
          </div>
        </div>

        <div className="field">
          <label htmlFor="f-img">Image URL</label>
          <input
            id="f-img"
            value={form.image_url}
            onChange={(e) => set("image_url", e.target.value)}
          />
        </div>

        <div className="field">
          <label htmlFor="f-desc">Description</label>
          <input
            id="f-desc"
            value={form.description}
            onChange={(e) => set("description", e.target.value)}
          />
        </div>

        <div className="checkbox-row">
          <input
            type="checkbox"
            id="f-available"
            checked={form.is_available}
            onChange={(e) => set("is_available", e.target.checked)}
          />
          <label htmlFor="f-available" style={{ margin: 0 }}>
            Available
          </label>
        </div>

        <div className="modal-actions">
          <button
            type="submit"
            className="btn btn-primary"
            disabled={submitting}
          >
            {submitting
              ? "Saving…"
              : existing
                ? "Save changes"
                : "Create supplier"}
          </button>
          <button
            type="button"
            className="btn btn-secondary"
            onClick={onCancel}
            disabled={submitting}
          >
            Cancel
          </button>
        </div>
      </form>
    </>
  );
}

function toInput(s: Supplier): SupplierInput {
  const { id: _id, created_at: _c, updated_at: _u, ...input } = s;
  return input;
}
