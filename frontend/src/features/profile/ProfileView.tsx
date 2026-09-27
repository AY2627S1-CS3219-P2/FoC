// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Profile screen for the session, fixture or real.
//   2026-09-21: email and contact render only when present — they come
//   from the profile call, which can fail while the session stays valid.
//   2026-09-22: the "Mock data · user-service" badge was unconditional and
//   is now tied to isMock, like LoginPage's. Against the gateway every field
//   on this page is real.
// Author review: Nigeltzy - Checked the simple generated converted file based on the prototype and requirements provided.
// The file was then updated thereafter according to changing requirements and development process. All seems valid when checked.

import { MockBadge } from "../../components/MockBadge";
import type { ActingMode, Session } from "../auth/types";

interface ProfileViewProps {
  session: Session;
  /** True only when VITE_USE_FIXTURES=true; see App's `usingGateway`. */
  isMock: boolean;
  mode: ActingMode;
  onModeChange: (mode: ActingMode) => void;
  onLogOut: () => void;
}

/**
 * Profile screen for the signed-in account. The requester/courier toggle is
 * presentation only: it does not check whether a switch is allowed.
 */
export function ProfileView({
  session,
  isMock,
  mode,
  onModeChange,
  onLogOut,
}: ProfileViewProps) {
  return (
    <section>
      <div className="page-head">
        <h1>Profile</h1>
      </div>

      {isMock && <MockBadge service="user-service" />}

      <div className="card">
        <div style={{ display: "flex", gap: 12, alignItems: "center" }}>
          <span className="avatar" style={{ width: 44, height: 44, fontSize: 15 }}>
            {session.initials}
          </span>
          <div>
            <div style={{ fontSize: 17, fontWeight: 700 }}>{session.username}</div>
            {session.email ? (
              <div className="balance-label">{session.email}</div>
            ) : null}
          </div>
        </div>

        {/* Every field here is view-only (F1.4.1), and the identifier and
            email cannot be changed (F1.4.3). Email and contact come from the
            profile call, not the token, so when that call fails the email
            row is hidden and contact shows "Not set". */}
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
          {session.email ? (
            <div>
              <dt>Email</dt>
              <dd>{session.email}</dd>
            </div>
          ) : null}
          <div>
            <dt>Role</dt>
            <dd>{session.role}</dd>
          </div>
        </dl>
        <p className="balance-label">
          Editing your username and contact details (F1.4.2) is not built
          here yet. user-service accepts it — its PUT profile route takes
          username, password and phone_num — so what is missing is this
          screen, not the service.
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
