// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Ported the prototype's supplier list, search, filter and admin CRUD
//   flow (loadSuppliers, renderGrid, renderCategoryOptions) to React.
//   2026-09-22: takes the suppliers client as a prop instead of importing the
//   module, now that it carries a per-session token. The admin checkbox's
//   label no longer claims to bypass access control — it cannot any more.
// Author review: PENDING — <reviewer to complete>

import { useCallback, useEffect, useRef, useState } from "react";
import type { SuppliersApi } from "./suppliersApi";
import type { Supplier, SupplierInput } from "./types";
import { Modal } from "../../components/Modal";
import { SupplierCard } from "./SupplierCard";
import { SupplierDetail } from "./SupplierDetail";
import { SupplierForm } from "./SupplierForm";
import type { ToastMessage } from "../../components/Toast";

interface SuppliersViewProps {
  /** Injected by App, which owns the token store the transport reads. */
  api: SuppliersApi;
  isAdmin: boolean;
  onAdminChange: (isAdmin: boolean) => void;
  onNotify: (message: ToastMessage) => void;
}

/** Which modal, if any, is open. Null means none. */
type ModalState =
  | { kind: "detail"; supplier: Supplier }
  | { kind: "create" }
  | { kind: "edit"; supplier: Supplier }
  | null;

export function SuppliersView({
  api,
  isAdmin,
  onAdminChange,
  onNotify,
}: SuppliersViewProps) {
  const [suppliers, setSuppliers] = useState<Supplier[]>([]);
  const [search, setSearch] = useState("");
  const [category, setCategory] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [modal, setModal] = useState<ModalState>(null);
  const [submitting, setSubmitting] = useState(false);

  /**
   * The category dropdown is populated from the unfiltered listing and then
   * held steady, so narrowing the results cannot remove the option you would
   * need to widen them again. Same intent as the prototype's state.allCategories.
   */
  const [categories, setCategories] = useState<string[]>([]);
  const categoriesLoaded = useRef(false);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const results = await api.listSuppliers({ search, category });
      setSuppliers(results);
      if (!categoriesLoaded.current && !search && !category) {
        categoriesLoaded.current = true;
        setCategories([...new Set(results.map((s) => s.type))].sort());
      }
    } catch (err) {
      setSuppliers([]);
      setError(err instanceof Error ? err.message : "Something went wrong.");
    } finally {
      setLoading(false);
    }
  }, [search, category]);

  // Debounced so typing in the search box does not fire a request per keystroke.
  useEffect(() => {
    const timer = setTimeout(load, 300);
    return () => clearTimeout(timer);
  }, [load]);

  const handleSubmit = async (input: SupplierInput) => {
    setSubmitting(true);
    try {
      if (modal?.kind === "edit") {
        await api.updateSupplier(modal.supplier.id, input);
        onNotify({ text: "Supplier updated", kind: "success" });
      } else {
        await api.createSupplier(input);
        onNotify({ text: "Supplier created", kind: "success" });
      }
      setModal(null);
      await load();
    } catch (err) {
      onNotify({
        text: err instanceof Error ? err.message : "Save failed",
        kind: "error",
      });
    } finally {
      setSubmitting(false);
    }
  };

  const handleDelete = async (supplier: Supplier) => {
    try {
      await api.deleteSupplier(supplier.id);
      onNotify({ text: "Supplier deleted", kind: "success" });
      setModal(null);
      await load();
    } catch (err) {
      onNotify({
        text: err instanceof Error ? err.message : "Delete failed",
        kind: "error",
      });
    }
  };

  return (
    <section>
      <div className="page-head">
        <h1>Suppliers</h1>
        <p className="page-sub">
          Campus stores, facilities and pickup points an errand can point at.
          This is the one screen backed by a real service.
        </p>
      </div>

      <div className="controls">
        <input
          className="search-input"
          type="search"
          placeholder="Search by name…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          aria-label="Search suppliers by name"
        />
        <select
          className="category-select"
          value={category}
          onChange={(e) => setCategory(e.target.value)}
          aria-label="Filter by category"
        >
          <option value="">All categories</option>
          {categories.map((c) => (
            <option key={c} value={c}>
              {c}
            </option>
          ))}
        </select>
        {isAdmin && (
          <button
            className="btn btn-primary"
            onClick={() => setModal({ kind: "create" })}
          >
            + Add supplier
          </button>
        )}
      </div>

      {/*
        A VIEW switch only, as of 2026-09-22. It shows and hides the admin
        controls; it no longer makes the client assert anything. suppliersApi
        used to send `X-User-Role: ADMIN` itself, which worked only while the
        browser reached supplier-service directly. Through the gateway that
        header is stripped and replaced with the role from the verified token
        (D-022), so ticking this box on a STUDENT account now surfaces the
        buttons and the writes come back 403 from supplier-service.
        Drive it from session.role and delete the checkbox once someone
        decides that is the behaviour they want.
      */}
      <label
        className="checkbox-row"
        title="Shows the admin controls. Whether they work is decided by supplier-service, from the role in your access token."
        style={{ marginBottom: 18 }}
      >
        <input
          type="checkbox"
          checked={isAdmin}
          onChange={(e) => onAdminChange(e.target.checked)}
        />
        <span className="balance-label">
          Show admin controls (an ADMIN account is still required to use them)
        </span>
      </label>

      {loading && <p className="status">Loading suppliers…</p>}

      {error && !loading && (
        <p className="status error">
          Could not load suppliers: {error}
          <br />
          <span className="status-hint">
            The browser reaches suppliers through the API Gateway (D-010), so
            this is the gateway not answering — which may mean the gateway
            itself, or supplier-service behind it.
          </span>
        </p>
      )}

      {!loading && !error && suppliers.length === 0 && (
        <p className="status">No suppliers match your search.</p>
      )}

      {!loading && !error && suppliers.length > 0 && (
        <div className="supplier-grid">
          {suppliers.map((s) => (
            <SupplierCard
              key={s.id}
              supplier={s}
              onSelect={(supplier) => setModal({ kind: "detail", supplier })}
            />
          ))}
        </div>
      )}

      {modal && (
        <Modal onClose={() => setModal(null)}>
          {modal.kind === "detail" && (
            <SupplierDetail
              supplier={modal.supplier}
              isAdmin={isAdmin}
              onEdit={(supplier) => setModal({ kind: "edit", supplier })}
              onDelete={handleDelete}
            />
          )}
          {modal.kind === "create" && (
            <SupplierForm
              submitting={submitting}
              onSubmit={handleSubmit}
              onCancel={() => setModal(null)}
            />
          )}
          {modal.kind === "edit" && (
            <SupplierForm
              existing={modal.supplier}
              submitting={submitting}
              onSubmit={handleSubmit}
              onCancel={() => setModal(null)}
            />
          )}
        </Modal>
      )}
    </section>
  );
}
