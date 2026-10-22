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
//
// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-11
// Scope: Username, contact number and password are now editable, each in its
//   own modal (owner's instruction), through user-service's
//   PUT /api/v1/users/{uid} (usersApi.ts).
// Author review: nigeltzy — Checked that the component was generated and
// updated according to the format and requirements that were previously discussed and set.

import { useState } from "react";
import { MockBadge } from "../../components/MockBadge";
import { Modal } from "../../components/Modal";
import type { ToastMessage } from "../../components/Toast";
import type { ActingMode, Session } from "../auth/types";
import { ChangePasswordForm, ContactForm, UsernameForm } from "./ProfileEditForms";
import type { UserProfile, UsersApi } from "./usersApi";

interface ProfileViewProps {
  session: Session;
  /** True only when VITE_USE_FIXTURES=true; see App's `usingGateway`. */
  isMock: boolean;
  /**
   * user-service's profile client, or null in a fixture build, which has no
   * real account to change. Null hides the edit controls.
   */
  usersApi: UsersApi | null;
  /** The saved profile after a username or contact change, for App's session. */
  onProfileUpdated: (profile: UserProfile) => void;
  /** user-service has ended every session; App signs the user out. */
  onPasswordChanged: () => void;
  onNotify: (message: ToastMessage) => void;
  mode: ActingMode;
  onModeChange: (mode: ActingMode) => void;
  onLogOut: () => void;
}

/** Which single-field modal is open, if any. */
type EditModal = "username" | "contact" | "password" | null;

/**
 * Profile screen for the signed-in account. The requester/courier toggle is
 * presentation only: it does not check whether a switch is allowed.
 */
export function ProfileView({
  session,
  isMock,
  usersApi,
  onProfileUpdated,
  onPasswordChanged,
  onNotify,
  mode,
  onModeChange,
  onLogOut,
}: ProfileViewProps) {
  const [editing, setEditing] = useState<EditModal>(null);
  const close = () => setEditing(null);

  async function saveUsername(username: string) {
    const profile = await usersApi!.changeOwnUsername(session.userId, username);
    onProfileUpdated(profile);
    close();
    onNotify({ text: "Username updated", kind: "success" });
  }

  async function saveContact(contact: string) {
    const profile = await usersApi!.changeOwnContact(session.userId, contact);
    onProfileUpdated(profile);
    close();
    onNotify({ text: "Contact number updated", kind: "success" });
  }

  async function savePassword(currentPassword: string, newPassword: string) {
    await usersApi!.changeOwnPassword(session.userId, currentPassword, newPassword);
    close();
    onPasswordChanged();
  }

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

        {/* The account identifier and email cannot be changed (F1.4.3);
            username (F1.4.2) and contact can, one at a time. Email and
            contact come from the profile call, not the token, so when that
            call fails the email row is hidden and contact shows "Not set". */}
        <h2 className="section-label">Account</h2>
        <dl className="detail-list">
          <div>
            <dt>Account ID</dt>
            <dd>{session.userId || "—"}</dd>
          </div>
          <div>
            <dt>Username</dt>
            <dd className="with-action">
              <span>{session.username}</span>
              {usersApi && (
                <button className="link-btn" onClick={() => setEditing("username")}>
                  Edit
                </button>
              )}
            </dd>
          </div>
          <div>
            <dt>Contact</dt>
            <dd className="with-action">
              <span>{session.contact || "Not set"}</span>
              {usersApi && (
                <button className="link-btn" onClick={() => setEditing("contact")}>
                  {session.contact ? "Edit" : "Add"}
                </button>
              )}
            </dd>
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
        {/* AI-generated (edited by nigeltzy). */}
        {usersApi ? (
          <div className="btn-row" style={{ justifyContent: "flex-start", marginTop: 4 }}>
            <button className="btn btn-secondary" onClick={() => setEditing("password")}>
              Change password
            </button>
          </div>
        ) : (
          <p className="balance-label">
            Editing your account isn't available in a fixture build.
          </p>
        )}

        {/* AI-generated (edited by James Teo): the acting mode is a student's
            only; an admin never posts or carries an errand. */}
        {session.role === "STUDENT" && (
          <>
            <h2 className="section-label" id="profile-acting-as">
              Acting as
            </h2>
            <div
              className="mode-toggle"
              style={{ display: "inline-flex" }}
              role="group"
              aria-labelledby="profile-acting-as"
            >
              <button
                className={mode === "requesting" ? "active" : ""}
                aria-pressed={mode === "requesting"}
                onClick={() => onModeChange("requesting")}
              >
                Requesting
              </button>
              <button
                className={mode === "delivering" ? "active" : ""}
                aria-pressed={mode === "delivering"}
                onClick={() => onModeChange("delivering")}
              >
                Delivering
              </button>
            </div>
            <p className="balance-label" style={{ marginTop: 8 }}>
              Switch to Delivering to find errands to run; switch back to Requesting to
              post your own.
            </p>
          </>
        )}

        <div className="btn-row">
          <button className="btn btn-secondary" onClick={onLogOut}>
            Log out
          </button>
        </div>
      </div>

      {/* AI-generated (edited by nigeltzy). One modal per field. */}
      {editing && usersApi && (
        <Modal onClose={close}>
          {editing === "username" && (
            <UsernameForm current={session.username} onSave={saveUsername} onCancel={close} />
          )}
          {editing === "contact" && (
            <ContactForm current={session.contact} onSave={saveContact} onCancel={close} />
          )}
          {editing === "password" && (
            <ChangePasswordForm onSave={savePassword} onCancel={close} />
          )}
        </Modal>
      )}
    </section>
  );
}
