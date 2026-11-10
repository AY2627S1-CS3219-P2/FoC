// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-11
// Scope: Admin dashboard, built to the owner's instruction — look up another
//   user's profile by account ID and reset that user's password, through
//   user-service's GET and PUT /api/v1/users/{uid} (profile/usersApi.ts).
// Author review: nigeltzy - Checked that the component was generated and 
// updated according to the format and requirements that were previously discussed and set.

// AI-generated (edited by James Teo): useEffect, for an account handed over
// from an errand's detail; useRef, UsersApiError and AccountStatus, for the
// suspend and reactivate control.
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
// AI-generated (edited by James Teo): the app's date format.
import { formatDateTime } from "../errands/time";

interface AdminViewProps {
  /** Null in a fixture build, which has no user-service to call. */
  usersApi: UsersApi | null;
  /** The signed-in admin's own ID, so the reset is not offered on it. */
  currentUserId: string;
  // AI-generated (edited by James Teo).
  /**
   * An account ID handed over from an errand's detail (Look up account),
   * filled in and looked up as the screen opens. App remounts this view for
   * each hand-over.
   */
  initialAccountId?: string | null;
  onNotify: (message: ToastMessage) => void;
  // AI-generated (edited by Claude Code, 2026-11-03): the dashboard's Accounts
  // tab shows an account's actions inside its own card, picked from the list.
  /**
   * Shown inside the dashboard's Accounts tab: no page heading, no ID lookup
   * form (the account is picked from the list), and no card of its own.
   */
  embedded?: boolean;
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
// AI-generated (edited by James Teo): initialAccountId.
export function AdminView({
  usersApi,
  currentUserId,
  initialAccountId,
  onNotify,
  embedded = false,
}: AdminViewProps) {
  const [query, setQuery] = useState(initialAccountId ?? "");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [profile, setProfile] = useState<UserProfile | null>(null);
  const [resetting, setResetting] = useState(false);
  // AI-generated (edited by James Teo): the account-status control. Its
  // confirmation step replaces the button row, and its buttons are disabled
  // while the change is in flight, so focus would be lost twice: focus goes
  // back to the control's button once the step closes, and to the confirm
  // button once a failed change re-enables it.
  // AI-generated (edited by Claude Code, 2026-11-09): the same step now serves
  // the role control (promote, demote) as well as the status one, so it holds
  // which action is being confirmed. Focus goes back to the button of that
  // action's family, since the button itself may have changed (Suspend becomes
  // Reactivate, Make admin becomes Remove admin).
  const [confirming, setConfirming] = useState<AccountActionKey | null>(null);
  const [changing, setChanging] = useState(false);
  const [statusError, setStatusError] = useState<string | null>(null);
  const statusButtonRef = useRef<HTMLButtonElement>(null);
  const roleButtonRef = useRef<HTMLButtonElement>(null);
  const refocusFamily = useRef<"status" | "role" | null>(null);
  useEffect(() => {
    if (confirming || !refocusFamily.current) return;
    const family = refocusFamily.current;
    refocusFamily.current = null;
    (family === "status" ? statusButtonRef : roleButtonRef).current?.focus();
  }, [confirming, profile]);
  const confirmButtonRef = useRef<HTMLButtonElement>(null);
  const refocusConfirmButton = useRef(false);
  useEffect(() => {
    if (changing || !refocusConfirmButton.current) return;
    refocusConfirmButton.current = false;
    confirmButtonRef.current?.focus();
  }, [changing]);

  // AI-generated (edited by James Teo): a handed-over account is looked up once,
  // as the screen opens.
  useEffect(() => {
    if (usersApi && initialAccountId) void findAccount(usersApi, initialAccountId.trim());
    // Once per mount, by design: App remounts this view for each hand-over.
  }, []);

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

  // AI-generated (edited by James Teo): the lookup itself moved into
  // findAccount, which the hand-over from an errand's detail also uses.
  async function lookUp(e: FormEvent) {
    e.preventDefault();
    await findAccount(api, query.trim());
  }

  async function findAccount(client: UsersApi, uid: string) {
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
    setConfirming(null);
    setStatusError(null);
    try {
      setProfile(await client.getUser(uid));
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
  const control = statusControlFor(profile?.status, isSelf, profile?.role);
  // AI-generated (edited by Claude Code, 2026-11-09): which role control this
  // account gets, beside the status one.
  const roleControl = roleControlFor(profile?.status, profile?.role, isSelf);
  const statusKey: AccountActionKey | null = control === "suspend" || control === "reactivate" ? control : null;
  const roleKey: AccountActionKey | null = roleControl === "none" ? null : roleControl;
  const statusAction = statusKey ? ACCOUNT_ACTIONS[statusKey] : undefined;
  const roleAction = roleKey ? ACCOUNT_ACTIONS[roleKey] : undefined;
  const action = confirming ? ACCOUNT_ACTIONS[confirming] : undefined;

  function cancelChange() {
    refocusFamily.current = confirming ? ACTION_FAMILY[confirming] : null;
    setConfirming(null);
    setStatusError(null);
  }

  async function changeAccount() {
    if (!profile || !confirming || !action) return;
    const target = profile;
    const family = ACTION_FAMILY[confirming];
    setChanging(true);
    setStatusError(null);
    try {
      await action.run(api, target.uid);
    } catch (err) {
      refocusConfirmButton.current = true;
      setStatusError(statusErrorMessage(err));
      setChanging(false);
      return;
    }
    onNotify({ text: action.done(target.username || "The user"), kind: "success" });
    try {
      setProfile(await api.getUser(target.uid));
    } catch {
      // The change succeeded, so the Status or Role row shows its result even
      // when the lookup that would confirm it fails.
      setProfile(action.apply(target));
    }
    refocusFamily.current = family;
    setConfirming(null);
    setChanging(false);
  }

  const Shell = embedded ? "div" : "section";
  return (
    <Shell>
      {!embedded && (
        <div className="page-head">
          <h1>Admin</h1>
        </div>
      )}

      <div className={embedded ? undefined : "card"}>
        {!embedded && <h2 className="section-label">Look up a user</h2>}
        {!embedded && (
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
            disabled={loading || changing || !query.trim()}
          >
            {loading ? "Looking up…" : "Look up"}
          </button>
        </form>
        )}

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
            ) : confirming && action ? (
              <div
                className={action.confirmBoxClass}
                role="group"
                aria-labelledby="status-confirm-question"
              >
                <p id="status-confirm-question">
                  {action.verb} <strong>{profile.username || "this account"}</strong>?{" "}
                  {action.consequence}
                </p>
                {statusError && (
                  <p className="status error" role="alert">
                    {statusError}
                  </p>
                )}
                <div className="btn-row" style={{ justifyContent: "flex-start", marginTop: 0 }}>
                  <button
                    ref={confirmButtonRef}
                    className={`btn ${action.buttonClass}`}
                    onClick={changeAccount}
                    disabled={changing}
                  >
                    {changing ? action.busyLabel : action.confirmLabel}
                  </button>
                  <button
                    className="btn btn-secondary"
                    onClick={cancelChange}
                    disabled={changing}
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
                  {statusAction && statusKey && (
                    <button
                      ref={statusButtonRef}
                      className={`btn ${statusAction.buttonClass}`}
                      onClick={() => setConfirming(statusKey)}
                    >
                      {statusAction.label}
                    </button>
                  )}
                  {roleAction && roleKey && (
                    <button
                      ref={roleButtonRef}
                      className={`btn ${roleAction.buttonClass}`}
                      onClick={() => setConfirming(roleKey)}
                    >
                      {roleAction.label}
                    </button>
                  )}
                </div>
                {control === "not-verified" && <p className="balance-label">Not verified yet.</p>}
                {control === "none" && profile.role === "ADMIN" && profile.status === "ACTIVE" && (
                  <p className="balance-label">
                    An admin account can&rsquo;t be suspended. Remove their admin role first.
                  </p>
                )}
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
    </Shell>
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
 * status. An ACTIVE admin account gets none (D-125): no admin can be suspended.
 * This only picks a button: user-service decides whether the change is allowed,
 * and still refuses suspending an admin.
 */
export function statusControlFor(
  status: AccountStatus | undefined,
  isSelf: boolean,
  role?: string,
): StatusControl {
  if (isSelf) return "none";
  if (status === "ACTIVE") return role === "ADMIN" ? "none" : "suspend";
  if (status === "SUSPENDED") return "reactivate";
  if (status === "UNVERIFIED") return "not-verified";
  return "none";
}

// AI-generated (edited by Claude Code, 2026-11-09): the role control — Make
// admin and Remove admin (D-149), on user-service's PATCH /api/v1/users/{uid}/role.

/** The role control the Admin screen shows for a looked-up account. */
export type RoleControl = "promote" | "demote" | "none";

/**
 * Picks the role control for an account. An ACTIVE student can be made an
 * admin, and an admin can be made a student, whatever their status. Your own
 * account gets none, and neither does an unverified or suspended student, or
 * a profile with no role. This only picks a button: user-service decides
 * whether the change is allowed, and still refuses demoting yourself or the
 * last active admin.
 */
export function roleControlFor(
  status: AccountStatus | undefined,
  role: string | undefined,
  isSelf: boolean,
): RoleControl {
  if (isSelf) return "none";
  if (role === "ADMIN") return "demote";
  if (role === "STUDENT" && status === "ACTIVE") return "promote";
  return "none";
}

/** Every action the Admin screen can confirm: two on the status, two on the role. */
export type AccountActionKey = "suspend" | "reactivate" | "promote" | "demote";

/** Which button a confirmation step returns focus to. */
const ACTION_FAMILY: Record<AccountActionKey, "status" | "role"> = {
  suspend: "status",
  reactivate: "status",
  promote: "role",
  demote: "role",
};

/** The wording and the call behind one account action. */
interface AccountAction {
  label: string;
  buttonClass: string;
  /** Danger colours for suspending and demoting; neutral ones for the rest. */
  confirmBoxClass: string;
  /** Starts the confirmation question, which goes on with the username. */
  verb: string;
  consequence: string;
  confirmLabel: string;
  busyLabel: string;
  run: (api: UsersApi, uid: string) => Promise<void>;
  /** The profile as it is once the call succeeds, for when it cannot be fetched again. */
  apply: (profile: UserProfile) => UserProfile;
  done: (name: string) => string;
}

const ACCOUNT_ACTIONS: Record<AccountActionKey, AccountAction> = {
  promote: {
    label: "Make admin",
    buttonClass: "btn-secondary",
    confirmBoxClass: "confirm-box neutral",
    verb: "Give admin rights to",
    // user-service signs the target out of every session when the role changes.
    consequence:
      "They can manage accounts, errands and suppliers, and read the audit log. They're signed out and must sign in again.",
    confirmLabel: "Yes, make admin",
    busyLabel: "Making admin…",
    run: (api, uid) => api.promoteUser(uid),
    apply: (profile) => ({ ...profile, role: "ADMIN" }),
    done: (name) => `${name} is now an admin.`,
  },
  demote: {
    label: "Remove admin",
    buttonClass: "btn-danger",
    confirmBoxClass: "confirm-box",
    verb: "Remove admin rights from",
    consequence:
      "They become a student and lose access to the dashboard. They're signed out and must sign in again.",
    confirmLabel: "Yes, remove admin",
    busyLabel: "Removing admin…",
    run: (api, uid) => api.demoteUser(uid),
    apply: (profile) => ({ ...profile, role: "STUDENT" }),
    done: (name) => `${name} is now a student.`,
  },
  suspend: {
    label: "Suspend account",
    buttonClass: "btn-danger",
    confirmBoxClass: "confirm-box",
    verb: "Suspend",
    // The gateway refuses a suspended account's token on its next request, so
    // the student is signed out then, not when the token expires.
    consequence:
      "They can't sign in again until you reactivate them, and they're signed out at their next action. Their errands don't change.",
    confirmLabel: "Yes, suspend",
    busyLabel: "Suspending…",
    run: (api, uid) => api.suspendUser(uid),
    apply: (profile) => ({ ...profile, status: "SUSPENDED" }),
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
    apply: (profile) => ({ ...profile, status: "ACTIVE" }),
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

// AI-generated (edited by James Teo): "18 Oct, 20:37", the errand screens'
// format, instead of the browser's locale string.
/**
 * A date and time as "18 Oct, 20:37", in the device's time zone unless
 * `timeZone` names one. An unreadable date is shown as sent.
 */
export function formatDate(iso?: string, timeZone?: string): string | undefined {
  if (!iso) return undefined;
  return formatDateTime(iso, timeZone) || iso;
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
