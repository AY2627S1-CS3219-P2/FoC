// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Profile screen against the mock user-service session.
// Author review: PENDING — <reviewer to complete>

import { MockBadge } from "../../components/MockBadge";
import type { ActingMode, Session } from "../auth/types";

interface ProfileViewProps {
  session: Session;
  mode: ActingMode;
  onModeChange: (mode: ActingMode) => void;
  onLogOut: () => void;
}

/**
 * The requester/courier toggle is rendered here and in the top bar, but the
 * rule about when it may be switched belongs to user-service and order-service
 * (user-service/AGENTS.md: "you may not switch to requester while a delivery is
 * in flight" is an order-service question). This is presentation only.
 */
export function ProfileView({
  session,
  mode,
  onModeChange,
  onLogOut,
}: ProfileViewProps) {
  return (
    <section>
      <div className="page-head">
        <h1>Profile</h1>
      </div>

      <MockBadge service="user-service" />

      <div className="card">
        <div style={{ display: "flex", gap: 12, alignItems: "center" }}>
          <span className="avatar" style={{ width: 44, height: 44, fontSize: 15 }}>
            {session.initials}
          </span>
          <div>
            <div style={{ fontSize: 17, fontWeight: 700 }}>{session.username}</div>
            <div className="balance-label">{session.email}</div>
          </div>
        </div>

        {/* F1.4.1 - the account identifier, registered email, username and
            contact information are all viewable. F1.4.3 makes the identifier
            and the email unchangeable, so they are shown as plain text. */}
        <h2 className="section-label">Account</h2>
        <dl className="detail-list">
          <div>
            <dt>Account ID</dt>
            <dd>{session.userId || "—"}</dd>
          </div>
          <div>
            <dt>Username</dt>
            <dd>{session.username}</dd>
          </div>
          <div>
            <dt>Contact</dt>
            <dd>{session.contact || "Not set"}</dd>
          </div>
          <div>
            <dt>Role</dt>
            <dd>{session.role}</dd>
          </div>
        </dl>
        <p className="balance-label">
          Editing your username and contact details (F1.4.2) is not built yet.
        </p>

        <h2 className="section-label">Acting as</h2>
        <div className="mode-toggle" style={{ display: "inline-flex" }}>
          <button
            className={mode === "requesting" ? "active" : ""}
            onClick={() => onModeChange("requesting")}
          >
            Requesting
          </button>
          <button
            className={mode === "delivering" ? "active" : ""}
            onClick={() => onModeChange("delivering")}
          >
            Delivering
          </button>
        </div>
        <p className="balance-label" style={{ marginTop: 8 }}>
          Requesters post errands; couriers fulfil them. Which switches are
          allowed, and when, is decided by user-service and order-service.
        </p>

        <div className="btn-row">
          <button className="btn btn-secondary" onClick={onLogOut}>
            Log out
          </button>
        </div>
      </div>
    </section>
  );
}
