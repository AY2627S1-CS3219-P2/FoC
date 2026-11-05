// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-03
// Scope: The rows of an admin audit log (D-107, D-113): when, who, what, the
//   outcome, and what changed or why it was refused. Shared by the Audit log tab
//   and an account's review.
// Author review: PENDING — <reviewer to complete>

import { formatDateTime } from "../errands/time";
import {
  actionLabel,
  auditDetails,
  AUDIT_SOURCE_LABEL,
  shortId,
  type AuditEntry,
  type AuditOutcome,
} from "./auditTypes";

const OUTCOME_BADGE: Record<AuditOutcome, string> = {
  succeeded: "badge-available",
  refused: "badge-type",
  failed: "badge-unavailable",
};

interface AuditListProps {
  entries: AuditEntry[];
  /** Opens that account's review; offered on a row whose target is an account. */
  onOpenAccount?: (uid: string) => void;
}

/** Audit rows, newest first as given. The caller shows loading, empty and failed states. */
export function AuditList({ entries, onOpenAccount }: AuditListProps) {
  return (
    <ul className="plain-list" style={{ margin: 0, padding: 0, listStyle: "none" }}>
      {entries.map((e) => {
        const details = auditDetails(e);
        return (
          <li key={`${e.source}-${e.id}`} className="audit-row">
            <div className="audit-head">
              <strong>{actionLabel(e.action)}</strong>
              <span className={`badge ${OUTCOME_BADGE[e.outcome]}`}>{e.outcome}</span>
              <span className="audit-meta" style={{ margin: 0 }}>
                {formatDateTime(e.occurredAt) || e.occurredAt}
              </span>
            </div>
            <p className="audit-meta">
              {AUDIT_SOURCE_LABEL[e.source]} · by <span title={e.actorId}>admin {shortId(e.actorId)}</span> · {e.targetType}{" "}
              <span title={e.targetId}>{e.targetId ? shortId(e.targetId) : "—"}</span>
              {onOpenAccount && e.targetType === "account" && e.targetId && (
                <>
                  {" · "}
                  <button type="button" className="link-btn" onClick={() => onOpenAccount(e.targetId)}>
                    Review account
                  </button>
                </>
              )}
            </p>
            {details.map((d) => (
              <p key={d.label} className="audit-detail">
                <strong>{d.label}:</strong> {d.value}
              </p>
            ))}
          </li>
        );
      })}
    </ul>
  );
}
