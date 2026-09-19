// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Built the My errands list against the mock order-service.
// Author review: PENDING — <reviewer to complete>

import { useEffect, useState } from "react";
import { MockBadge } from "../../components/MockBadge";
import type { ActingMode } from "../auth/types";
import * as errandsApi from "./errandsApi";
import { TERMINAL_STATUSES, type Errand, type ErrandStatus } from "./types";

interface MyErrandsViewProps {
  /** Requesting shows errands you posted; Delivering, ones you could fulfil. */
  mode: ActingMode;
}

/** Display text for each state in the team glossary's order-state set. */
const STATUS_LABEL: Record<ErrandStatus, string> = {
  OPEN: "Open",
  ACCEPTED: "Accepted",
  PICKED_UP: "Picked up",
  DELIVERED: "Delivered",
  COMPLETED: "Completed",
  CANCELLED: "Cancelled",
  EXPIRED: "Expired",
};

/** Purely visual grouping — it decides nothing about what a state permits. */
function badgeClass(status: ErrandStatus): string {
  if (status === "COMPLETED" || status === "DELIVERED") return "badge-available";
  if (TERMINAL_STATUSES.includes(status)) return "badge-unavailable";
  return "badge-type";
}

export function MyErrandsView({ mode }: MyErrandsViewProps) {
  const [errands, setErrands] = useState<Errand[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    errandsApi.listErrands().then((result) => {
      if (cancelled) return;
      setErrands(result);
      setLoading(false);
    });
    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <section>
      <div className="page-head">
        <h1>My errands</h1>
        <p className="page-sub">
          {mode === "requesting"
            ? "Errands you have posted. Credits stay held until one is delivered, cancelled or expires."
            : "Errands you have taken on as a courier."}
        </p>
      </div>

      <MockBadge service="order-service" />

      {loading && <p className="status">Loading errands…</p>}

      {!loading && errands.length === 0 && (
        <p className="status">
          No errands yet. Post one from <strong>New errand</strong>.
        </p>
      )}

      {!loading &&
        errands.map((errand) => (
          <article className="errand-card" key={errand.id}>
            <div>
              <div className="errand-title">
                {errand.items.map((i) => i.text).join(", ")}
              </div>
              <div className="errand-meta">
                {errand.id} · {errand.supplierName} → {errand.deliverTo}
              </div>
            </div>
            <div style={{ display: "flex", gap: 12, alignItems: "center" }}>
              <span className={`badge ${badgeClass(errand.status)}`}>
                {STATUS_LABEL[errand.status]}
              </span>
              <span className="errand-reward">{errand.reward} cr</span>
            </div>
          </article>
        ))}
    </section>
  );
}
