// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-11
// Scope: Admin dashboard, built to the owner's instruction — look up another
//   user's profile by account ID and reset that user's password, through
//   user-service's GET and PUT /api/v1/users/{uid} (profile/usersApi.ts).
// Author review: nigeltzy - Checked that the component was generated and 
// updated according to the format and requirements that were previously discussed and set.

import { useState, type FormEvent } from "react";
import { MockBadge } from "../../components/MockBadge";
import { Modal } from "../../components/Modal";
import type { ToastMessage } from "../../components/Toast";
import { NewPasswordFields, validateNewPassword } from "../profile/ProfileEditForms";
import type { UserProfile, UsersApi } from "../profile/usersApi";

interface AdminViewProps {
  /** Null in a fixture build, which has no user-service to call. */
  usersApi: UsersApi | null;
  /** The signed-in admin's own ID, so the reset is not offered on it. */
  currentUserId: string;
  onNotify: (message: ToastMessage) => void;
}

/**
 * App renders this only for an ADMIN session, which is an affordance and not
 * access control. user-service decides what a lookup returns (full profile
 * for an ADMIN token, the restricted one otherwise) and refuses a password
 * reset from anyone else.
 *
 * Lookup is by account ID because that is the only key user-service's API
 * offers: it has no endpoint to list or search users.
 */
export function AdminView({ usersApi, currentUserId, onNotify }: AdminViewProps) {
  const [query, setQuery] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [profile, setProfile] = useState<UserProfile | null>(null);
  const [resetting, setResetting] = useState(false);

  if (!usersApi) {
    return (
      <section>
        <div className="page-head">
          <h1>Admin</h1>
        </div>
        <MockBadge service="user-service" />
        <p className="status">The admin tools need user-service, so a fixture build cannot use them.</p>
      </section>
    );
  }
  const api = usersApi;

  async function lookUp(e: FormEvent) {
    e.preventDefault();
    const uid = query.trim();
    // UX only: user-service answers 404 to anything that is not a UUID.
    if (!UUID_PATTERN.test(uid)) {
      setError("Enter a full account ID, like 3f2b8c1e-…, as shown on that user's Profile.");
      return;
    }
    setLoading(true);
    setError(null);
    setProfile(null);
    try {
      setProfile(await api.getUser(uid));
    } catch (err) {
      setError(err instanceof Error ? err.message : "The lookup failed. Try again.");
    } finally {
      setLoading(false);
    }
  }

  async function resetPassword(newPassword: string) {
    const updated = await api.resetPassword(profile!.uid, newPassword);
    setProfile(updated);
    setResetting(false);
    onNotify({
      text: `Password reset. ${updated.username || "The user"} is signed out everywhere and must log in with the new password.`,
      kind: "success",
    });
  }

  const isSelf = profile?.uid === currentUserId;

  return (
    <section>
      <div className="page-head">
        <h1>Admin</h1>
      </div>

      <div className="card">
        <h2 className="section-label">Look up a user</h2>
        <form className="controls" onSubmit={lookUp}>
          <input
            className="search-input"
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Account ID"
            aria-label="Account ID"
            spellCheck={false}
            autoComplete="off"
          />
          <button className="btn btn-primary" disabled={loading || !query.trim()}>
            {loading ? "Looking up…" : "Look up"}
          </button>
        </form>

        {error && <p className="status error">{error}</p>}

        {profile && (
          <>
            <dl className="detail-list">
              <Row label="Account ID" value={profile.uid} />
              <Row label="Username" value={profile.username} />
              <Row label="Email" value={profile.email} />
              <Row label="Contact" value={profile.contact} />
              <Row label="Role" value={profile.role} />
              <Row label="Status" value={profile.status} />
              <Row label="Created" value={formatDate(profile.createdAt)} />
            </dl>

            {isSelf ? (
              <p className="balance-label">
                This is your own account. Change your password from your Profile instead.
              </p>
            ) : (
              <div className="btn-row" style={{ justifyContent: "flex-start" }}>
                <button className="btn btn-secondary" onClick={() => setResetting(true)}>
                  Reset password
                </button>
              </div>
            )}
          </>
        )}
      </div>

      {resetting && profile && (
        <Modal onClose={() => setResetting(false)}>
          <ResetPasswordForm
            username={profile.username}
            onSave={resetPassword}
            onCancel={() => setResetting(false)}
          />
        </Modal>
      )}
    </section>
  );
}

/** A canonical 8-4-4-4-12 hex UUID, any case. */
const UUID_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

function Row({ label, value }: { label: string; value?: string }) {
  return (
    <div>
      <dt>{label}</dt>
      <dd>{value || "—"}</dd>
    </div>
  );
}

function formatDate(iso?: string): string | undefined {
  if (!iso) return undefined;
  const date = new Date(iso);
  return Number.isNaN(date.getTime()) ? iso : date.toLocaleString();
}

interface ResetPasswordFormProps {
  username: string;
  onSave: (newPassword: string) => Promise<void>;
  onCancel: () => void;
}

/** Sets another user's password without their current one (ADMIN only). */
function ResetPasswordForm({ username, onSave, onCancel }: ResetPasswordFormProps) {
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit(e: FormEvent) {
    e.preventDefault();
    const problem = validateNewPassword(password, confirm);
    if (problem) return setError(problem);
    setBusy(true);
    setError(null);
    try {
      await onSave(password);
    } catch (err) {
      setError(err instanceof Error ? err.message : "The reset failed. Try again.");
      setBusy(false);
    }
  }

  return (
    <>
      <h2 className="detail-title">Reset password for {username || "this user"}</h2>
      <form onSubmit={submit}>
        <NewPasswordFields
          idPrefix="reset"
          password={password}
          confirm={confirm}
          onPasswordChange={setPassword}
          onConfirmChange={setConfirm}
        />
        <p className="status-hint">
          This signs {username || "the user"} out on every device.
        </p>
        {error && <p className="status error">{error}</p>}
        <div className="btn-row">
          <button type="submit" className="btn btn-primary" disabled={busy}>
            {busy ? "Resetting…" : "Reset password"}
          </button>
          <button type="button" className="btn btn-secondary" onClick={onCancel} disabled={busy}>
            Cancel
          </button>
        </div>
      </form>
    </>
  );
}
