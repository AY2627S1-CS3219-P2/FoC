// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-11
// Scope: Admin dashboard, built to the owner's instruction — look up another
//   user's profile by account ID and reset that user's password, through
//   user-service's GET and PUT /api/v1/users/{uid} (profile/usersApi.ts).
// Author review: nigeltzy - Checked that the component was generated and 
// updated according to the format and requirements that were previously discussed and set.

// AI-generated (edited by James Teo): useEffect, useRef, UsersApiError and
// AccountStatus, for the suspend and reactivate control.
import { useEffect, useRef, useState, type FormEvent } from "react";
import { MockBadge } from "../../components/MockBadge";
import { Modal } from "../../components/Modal";
import type { ToastMessage } from "../../components/Toast";
import { NewPasswordFields, validateNewPassword } from "../profile/ProfileEditForms";
import {
  UsersApiError,
  type AccountStatus,
  type UserProfile,
  type UsersApi,
} from "../profile/usersApi";

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
  // AI-generated (edited by James Teo): the account-status control. Its
  // confirmation step replaces the button row, and its buttons are disabled
  // while the change is in flight, so focus would be lost twice: focus goes
  // back to the control's button once the step closes, and to the confirm
  // button once a failed change re-enables it.
  const [confirmingStatus, setConfirmingStatus] = useState(false);
  const [changingStatus, setChangingStatus] = useState(false);
  const [statusError, setStatusError] = useState<string | null>(null);
  const statusButtonRef = useRef<HTMLButtonElement>(null);
  const refocusStatusButton = useRef(false);
  useEffect(() => {
    if (confirmingStatus || !refocusStatusButton.current) return;
    refocusStatusButton.current = false;
    statusButtonRef.current?.focus();
  }, [confirmingStatus, profile]);
  const confirmButtonRef = useRef<HTMLButtonElement>(null);
  const refocusConfirmButton = useRef(false);
  useEffect(() => {
    if (changingStatus || !refocusConfirmButton.current) return;
    refocusConfirmButton.current = false;
    confirmButtonRef.current?.focus();
  }, [changingStatus]);

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
    // AI-generated (edited by James Teo): a new lookup closes any open
    // confirmation step, so it cannot act on the next account shown.
    setConfirmingStatus(false);
    setStatusError(null);
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
  // AI-generated (edited by James Teo): which status control this account gets,
  // and the handlers for its confirmation step.
  const control = statusControlFor(profile?.status, isSelf);
  const statusAction = STATUS_ACTIONS[control];

  function cancelStatusChange() {
    setConfirmingStatus(false);
    setStatusError(null);
    refocusStatusButton.current = true;
  }

  async function changeStatus() {
    if (!profile || !statusAction) return;
    const target = profile;
    setChangingStatus(true);
    setStatusError(null);
    try {
      await statusAction.run(api, target.uid);
    } catch (err) {
      refocusConfirmButton.current = true;
      setStatusError(statusErrorMessage(err));
      setChangingStatus(false);
      return;
    }
    onNotify({ text: statusAction.done(target.username || "The user"), kind: "success" });
    try {
      setProfile(await api.getUser(target.uid));
    } catch {
      // The change succeeded, so the Status row shows its result even when
      // the lookup that would confirm it fails.
      setProfile({ ...target, status: statusAction.result });
    }
    refocusStatusButton.current = true;
    setConfirmingStatus(false);
    setChangingStatus(false);
  }

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
          {/* AI-generated (edited by James Teo): also disabled while a status
              change is in flight, so its result cannot land on another account. */}
          <button
            className="btn btn-primary"
            disabled={loading || changingStatus || !query.trim()}
          >
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

            {/* AI-generated (edited by James Teo): the status control sits beside
                Reset password; its confirmation step replaces that row, as
                SupplierDetail's delete step does. A failed change shows its
                error inside the step, above the buttons, so it stays beside
                the action on a narrow screen. */}
            {isSelf ? (
              <p className="balance-label">
                This is your own account. Change your password from your Profile instead.
              </p>
            ) : confirmingStatus && statusAction ? (
              <div
                className={statusAction.confirmBoxClass}
                role="group"
                aria-labelledby="status-confirm-question"
              >
                <p id="status-confirm-question">
                  {statusAction.verb} <strong>{profile.username || "this account"}</strong>?{" "}
                  {statusAction.consequence}
                </p>
                {statusError && (
                  <p className="status error" role="alert">
                    {statusError}
                  </p>
                )}
                <div className="btn-row" style={{ justifyContent: "flex-start", marginTop: 0 }}>
                  <button
                    ref={confirmButtonRef}
                    className={`btn ${statusAction.buttonClass}`}
                    onClick={changeStatus}
                    disabled={changingStatus}
                  >
                    {changingStatus ? statusAction.busyLabel : statusAction.confirmLabel}
                  </button>
                  <button
                    className="btn btn-secondary"
                    onClick={cancelStatusChange}
                    disabled={changingStatus}
                    autoFocus
                  >
                    Cancel
                  </button>
                </div>
              </div>
            ) : (
              <>
                <div className="btn-row" style={{ justifyContent: "flex-start" }}>
                  <button className="btn btn-secondary" onClick={() => setResetting(true)}>
                    Reset password
                  </button>
                  {statusAction && (
                    <button
                      ref={statusButtonRef}
                      className={`btn ${statusAction.buttonClass}`}
                      onClick={() => setConfirmingStatus(true)}
                    >
                      {statusAction.label}
                    </button>
                  )}
                </div>
                {control === "not-verified" && <p className="balance-label">Not verified yet.</p>}
              </>
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

// AI-generated (edited by James Teo): the suspend and reactivate control —
// which one an account gets, and the wording and call for each.

/**
 * The account-status control the Admin screen shows for a looked-up account.
 * `not-verified` shows no control, only a note saying so.
 */
export type StatusControl = "suspend" | "reactivate" | "not-verified" | "none";

/**
 * Picks the status control for an account. ACTIVE accounts can be suspended
 * and SUSPENDED ones reactivated. An UNVERIFIED account gets no control,
 * because setting it ACTIVE would skip its email code. Your own account gets
 * none either, as with Reset password, and neither does a profile without a
 * status. This only picks a button: user-service decides whether the change
 * is allowed.
 */
export function statusControlFor(
  status: AccountStatus | undefined,
  isSelf: boolean,
): StatusControl {
  if (isSelf) return "none";
  if (status === "ACTIVE") return "suspend";
  if (status === "SUSPENDED") return "reactivate";
  if (status === "UNVERIFIED") return "not-verified";
  return "none";
}

/** The wording and the call behind one status control. */
interface StatusAction {
  label: string;
  buttonClass: string;
  /** Danger colours for suspending; neutral ones for reactivating. */
  confirmBoxClass: string;
  /** Starts the confirmation question, which goes on with the username. */
  verb: string;
  consequence: string;
  confirmLabel: string;
  busyLabel: string;
  run: (api: UsersApi, uid: string) => Promise<void>;
  /** The status the account has once the call succeeds. */
  result: AccountStatus;
  done: (name: string) => string;
}

const STATUS_ACTIONS: Partial<Record<StatusControl, StatusAction>> = {
  suspend: {
    label: "Suspend account",
    buttonClass: "btn-danger",
    confirmBoxClass: "confirm-box",
    verb: "Suspend",
    // What user-service does today: login and refresh are refused, but an
    // access token already issued works until it expires. That lifetime is
    // user-service's JWT_ACCESS_TOKEN_TTL setting, so the wording names no
    // time that could stop matching it.
    consequence:
      "They can't sign in again until you reactivate them, and their current session ends when it next refreshes. Their errands don't change.",
    confirmLabel: "Yes, suspend",
    busyLabel: "Suspending…",
    run: (api, uid) => api.suspendUser(uid),
    result: "SUSPENDED",
    done: (name) => `${name} is suspended.`,
  },
  reactivate: {
    label: "Reactivate account",
    buttonClass: "btn-primary",
    confirmBoxClass: "confirm-box neutral",
    verb: "Reactivate",
    consequence: "They can sign in again with their password.",
    confirmLabel: "Yes, reactivate",
    busyLabel: "Reactivating…",
    run: (api, uid) => api.reactivateUser(uid),
    result: "ACTIVE",
    done: (name) => `${name} can sign in again.`,
  },
};

/**
 * user-service's message as it sends it (the last-admin 409, a 403, a 404),
 * or a plain one when no response arrived.
 */
function statusErrorMessage(err: unknown): string {
  if (err instanceof UsersApiError && err.status === 0) {
    return "Couldn't reach the server. Try again.";
  }
  return err instanceof Error ? err.message : "The change failed. Try again.";
}

function Row({ label, value }: { label: string; value?: string }) {
  return (
    <div>
      <dt>{label}</dt>
      <dd>{value || "—"}</dd>
    </div>
  );
}

// AI-generated (edited by James Teo): the Credits screen's date format instead
// of the browser's locale string.
/**
 * A date and time as "18 Oct, 20:37", in the device's time zone unless
 * `timeZone` names one. An unreadable date is shown as sent.
 */
export function formatDate(iso?: string, timeZone?: string): string | undefined {
  if (!iso) return undefined;
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return iso;
  const day = date.toLocaleDateString("en-SG", { day: "numeric", month: "short", timeZone });
  const time = date.toLocaleTimeString("en-SG", {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
    timeZone,
  });
  return `${day}, ${time}`;
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
