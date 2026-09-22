// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Built the Request an errand screen from the owner's mockup. Supplier
//   list comes from the real supplier-service; posting goes to the mock
//   order-service.
// Author review: PENDING — <reviewer to complete>

import { useEffect, useState } from "react";
import { MockBadge } from "../../components/MockBadge";
import { SearchIcon } from "../../components/icons";
import type { ToastMessage } from "../../components/Toast";
import type { SuppliersApi } from "../suppliers/suppliersApi";
import type { Supplier } from "../suppliers/types";
import * as errandsApi from "./errandsApi";

interface NewErrandViewProps {
  /** Injected by App, which owns the token store the transport reads. */
  suppliersApi: SuppliersApi;
  /** Spendable credits, for the "leaves you N available" hint. */
  available: number;
  onNotify: (message: ToastMessage) => void;
  onPosted: () => void;
}

/**
 * Composes two services in one view, which frontend/AGENTS.md explicitly
 * allows: suppliers are read from supplier-service through its own client
 * (injected as a prop since 2026-09-22, because it now carries a token),
 * the errand is posted through order-service's. Each call goes through that
 * service's module; neither knows about the other.
 *
 * Campus delivery locations are hard-coded here because no service owns them
 * yet. Where they should live is an open question for the team.
 */
const DELIVERY_LOCATIONS = [
  "COM1 Level 1 study benches",
  "COM3 Lobby",
  "Central Library entrance",
  "UTown Residences lobby",
  "The Deck, AS8",
  "Science Frontier Food Court",
];

export function NewErrandView({
  suppliersApi,
  available,
  onNotify,
  onPosted,
}: NewErrandViewProps) {
  const [suppliers, setSuppliers] = useState<Supplier[]>([]);
  const [supplierError, setSupplierError] = useState<string | null>(null);
  const [search, setSearch] = useState("");
  const [selected, setSelected] = useState<Supplier | null>(null);

  const [items, setItems] = useState<string[]>([""]);
  const [deliverTo, setDeliverTo] = useState(DELIVERY_LOCATIONS[0]!);
  const [findMe, setFindMe] = useState("");
  const [reward, setReward] = useState(10);
  const [posting, setPosting] = useState(false);

  useEffect(() => {
    let cancelled = false;
    const timer = setTimeout(() => {
      suppliersApi
        .listSuppliers({ search })
        .then((result) => {
          if (!cancelled) {
            setSuppliers(result);
            setSupplierError(null);
          }
        })
        .catch((err: unknown) => {
          if (!cancelled) {
            setSuppliers([]);
            setSupplierError(
              err instanceof Error ? err.message : "Could not load suppliers.",
            );
          }
        });
    }, 300);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [search]);

  const setItem = (index: number, value: string) =>
    setItems((prev) => prev.map((item, i) => (i === index ? value : item)));

  const filledItems = items.map((i) => i.trim()).filter(Boolean);
  const canPost = selected !== null && filledItems.length > 0 && !posting;

  const post = async () => {
    if (!selected) return;
    setPosting(true);
    try {
      await errandsApi.createErrand({
        supplierId: selected.id,
        supplierName: selected.name,
        items: filledItems,
        deliverTo,
        findMe,
        reward,
      });
      onNotify({ text: `Errand posted · ${reward} credits held`, kind: "success" });
      onPosted();
    } catch (err) {
      onNotify({
        text: err instanceof Error ? err.message : "Could not post errand",
        kind: "error",
      });
    } finally {
      setPosting(false);
    }
  };

  return (
    <section>
      <div className="page-head">
        <h1>Request an errand</h1>
        <p className="page-sub">
          Credits are held the moment you post, and released if nobody accepts
          within 2 hours.
        </p>
      </div>

      <MockBadge service="order-service" />

      <div className="two-col">
        <div>
          <label className="field-label" htmlFor="supplier-search">
            Collect from
          </label>
          <div style={{ position: "relative", marginBottom: 12 }}>
            <span
              style={{
                position: "absolute",
                top: 10,
                left: 11,
                color: "var(--text-muted)",
              }}
            >
              <SearchIcon />
            </span>
            <input
              id="supplier-search"
              type="search"
              style={{ paddingLeft: 36 }}
              placeholder="Search suppliers and campus locations"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
          </div>

          {supplierError && (
            <p className="status error">
              Could not load suppliers: {supplierError}
              <br />
              <span className="status-hint">
                The browser reaches suppliers through the API Gateway
                (D-010), so this is the gateway not answering — which may
                mean the gateway itself, or supplier-service behind it.
              </span>
            </p>
          )}

          {!supplierError && (
            <div className="picker">
              {suppliers.length === 0 && (
                <div style={{ padding: 14, color: "var(--text-muted)" }}>
                  No suppliers match that search.
                </div>
              )}
              {suppliers.map((supplier) => (
                <button
                  key={supplier.id}
                  className={`picker-row ${
                    selected?.id === supplier.id ? "selected" : ""
                  }`}
                  onClick={() => setSelected(supplier)}
                >
                  <span className="tag-building">{supplier.building}</span>
                  <span>
                    <span className="picker-row-name">{supplier.name}</span>
                    <br />
                    <span className="picker-row-meta">
                      {supplier.type}
                      {supplier.opening_time && supplier.closing_time
                        ? ` · ${supplier.opening_time}–${supplier.closing_time}`
                        : ""}
                    </span>
                  </span>
                </button>
              ))}
            </div>
          )}

          <label className="field-label" style={{ marginTop: 18 }}>
            What to get
          </label>
          <div className="item-list">
            {items.map((item, index) => (
              <div className="item-row" key={index}>
                <input
                  value={item}
                  placeholder="1 × Iced latte, less ice"
                  onChange={(e) => setItem(index, e.target.value)}
                />
                {items.length > 1 && (
                  <button
                    className="btn btn-secondary"
                    onClick={() =>
                      setItems((prev) => prev.filter((_, i) => i !== index))
                    }
                    aria-label={`Remove item ${index + 1}`}
                  >
                    ×
                  </button>
                )}
              </div>
            ))}
          </div>
          <button
            className="btn btn-secondary"
            style={{ marginTop: 10 }}
            onClick={() => setItems((prev) => [...prev, ""])}
          >
            + Add item
          </button>
        </div>

        <div>
          <label className="field-label">Deliver to</label>
          <div className="field">
            <label htmlFor="deliver-to">Campus location</label>
            <select
              id="deliver-to"
              value={deliverTo}
              onChange={(e) => setDeliverTo(e.target.value)}
            >
              {DELIVERY_LOCATIONS.map((loc) => (
                <option key={loc} value={loc}>
                  {loc}
                </option>
              ))}
            </select>
          </div>

          <div className="field">
            <label htmlFor="find-me">
              How to find you <span className="field-optional">optional</span>
            </label>
            <textarea
              id="find-me"
              value={findMe}
              placeholder="Third table from the window, grey backpack"
              onChange={(e) => setFindMe(e.target.value)}
            />
          </div>

          <label className="field-label">Reward</label>
          <div className="stepper">
            <button
              onClick={() => setReward((r) => Math.max(1, r - 1))}
              aria-label="Decrease reward"
            >
              −
            </button>
            <div>
              <div className="stepper-value">{reward}</div>
              <div className="stepper-unit">credits</div>
            </div>
            <button
              onClick={() => setReward((r) => r + 1)}
              aria-label="Increase reward"
            >
              +
            </button>
          </div>
          <p className="balance-label" style={{ margin: "8px 0 18px" }}>
            Leaves you {Math.max(0, available - reward)} credits available while
            this errand is open.
          </p>

          <div className="card">
            <dl style={{ margin: 0 }}>
              <div className="summary-row">
                <dt>Collect from</dt>
                <dd>{selected ? selected.name : "Not chosen"}</dd>
              </div>
              <div className="summary-row">
                <dt>Deliver to</dt>
                <dd>{deliverTo}</dd>
              </div>
              <div className="summary-row">
                <dt>Expires</dt>
                <dd>2 hours after posting</dd>
              </div>
            </dl>

            <div className="btn-row">
              <button className="btn btn-secondary" onClick={onPosted}>
                Cancel
              </button>
              <button
                className="btn btn-primary"
                disabled={!canPost}
                onClick={post}
              >
                {posting ? "Posting…" : `Post errand · hold ${reward} credits`}
              </button>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}
