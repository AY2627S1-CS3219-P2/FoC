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
  /** False once a gateway is configured — see App's `usingGateway`. */
  isMock: boolean;
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

      {/*
        Conditional since 2026-09-22. This was rendered unconditionally from
        when the screen was written (17 Sep), because there was no
        user-service then. There is now: against the gateway every field
        below comes from the access token's claims plus
        GET /api/v1/users/{uid}, so the badge was claiming invented data on a
        page that has none. LoginPage has always gated it this way.
      */}
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

        {/* F1.4.1 - the account identifier, registered email, username and
            contact information are all viewable. F1.4.3 makes the identifier
            and the email unchangeable, so they are shown as plain text.

            Email and contact render only when present. user-service supplies
            both (`email` and `phone_num`, on the restricted response too), but
            they arrive on the profile call that follows login rather than in a
            token claim, so a failed call leaves them undefined while the
            session itself stays valid. */}
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
