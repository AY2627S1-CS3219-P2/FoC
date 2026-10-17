// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-11
// Scope: The single-field forms shown in modals on the Profile screen —
//   username, contact number, password — and the new-password fields the
//   admin reset form reuses. Built to the owner's instruction that each
//   field is changed on its own, one modal at a time.
// Author review: nigeltzy - Checked that the tsx form was generated according to the format and requirements that were previously discussed and set.

import { useState, type FormEvent } from "react";
import {
  CONTACT_MAX,
  passwordChecks,
  USERNAME_MAX,
  validateContact,
  validatePassword,
  validateUsername,
} from "../auth/validation";

/**
 * Each form validates for UX only, then hands the value to `onSave`. A
 * rejected `onSave` is shown inside the modal so the user can correct it
 * there; a resolved one is the parent's cue to close the modal. user-service
 * enforces every rule again, and its message is what the user sees.
 */

/** Runs a save, tracking in-flight state and the message to show on failure. */
function useSave() {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function run(save: () => Promise<void>) {
    setBusy(true);
    setError(null);
    try {
      await save();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Something went wrong. Try again.");
      setBusy(false);
    }
    // On success the parent unmounts this form, so busy is left as is.
  }

  return { busy, error, setError, run };
}

interface FormActionsProps {
  busy: boolean;
  disabled?: boolean;
  label: string;
  busyLabel: string;
  onCancel: () => void;
}

function FormActions({ busy, disabled, label, busyLabel, onCancel }: FormActionsProps) {
  return (
    <div className="btn-row">
      <button type="submit" className="btn btn-primary" disabled={busy || disabled}>
        {busy ? busyLabel : label}
      </button>
      <button type="button" className="btn btn-secondary" onClick={onCancel} disabled={busy}>
        Cancel
      </button>
    </div>
  );
}

interface UsernameFormProps {
  current: string;
  onSave: (username: string) => Promise<void>;
  onCancel: () => void;
}

/** F1.4.2 — changes the username. Uniqueness is user-service's to check (409). */
export function UsernameForm({ current, onSave, onCancel }: UsernameFormProps) {
  const [username, setUsername] = useState(current);
  const { busy, error, setError, run } = useSave();
  const unchanged = username.trim() === current;

  const submit = (e: FormEvent) => {
    e.preventDefault();
    const problem = validateUsername(username);
    if (problem) return setError(problem);
    void run(() => onSave(username.trim()));
  };

  return (
    <>
      <h2 className="detail-title">Change username</h2>
      <form onSubmit={submit}>
        <div className="field">
          <label htmlFor="edit-username">Username</label>
          <input
            id="edit-username"
            type="text"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            autoComplete="username"
            maxLength={USERNAME_MAX}
            required
            autoFocus
          />
          <p className="status-hint">
            Letters and numbers only. You'll log in with the new username from now on.
          </p>
        </div>
        {error && <p className="status error">{error}</p>}
        <FormActions
          busy={busy}
          disabled={unchanged}
          label="Save username"
          busyLabel="Saving…"
          onCancel={onCancel}
        />
      </form>
    </>
  );
}

interface ContactFormProps {
  current?: string;
  onSave: (contact: string) => Promise<void>;
  onCancel: () => void;
}

/** F1.4.1 contact information — user-service's `phone_num`. */
export function ContactForm({ current = "", onSave, onCancel }: ContactFormProps) {
  const [contact, setContact] = useState(current);
  const { busy, error, setError, run } = useSave();
  const unchanged = contact.trim() === current;

  const submit = (e: FormEvent) => {
    e.preventDefault();
    const problem = validateContact(contact);
    if (problem) return setError(problem);
    void run(() => onSave(contact.trim()));
  };

  return (
    <>
      <h2 className="detail-title">{current ? "Change contact number" : "Add contact number"}</h2>
      <form onSubmit={submit}>
        <div className="field">
          <label htmlFor="edit-contact">Contact number</label>
          <input
            id="edit-contact"
            type="tel"
            value={contact}
            onChange={(e) => setContact(e.target.value)}
            placeholder="9123 4567"
            autoComplete="tel"
            maxLength={CONTACT_MAX}
            required
            autoFocus
          />
        </div>
        {error && <p className="status error">{error}</p>}
        <FormActions
          busy={busy}
          disabled={unchanged}
          label="Save contact number"
          busyLabel="Saving…"
          onCancel={onCancel}
        />
      </form>
    </>
  );
}

interface NewPasswordFieldsProps {
  idPrefix: string;
  password: string;
  confirm: string;
  onPasswordChange: (value: string) => void;
  onConfirmChange: (value: string) => void;
}

/**
 * New password plus confirmation, with F1.1.3.1's rules shown live as on the
 * signup form. Shared by the self-service change and the admin reset.
 */
export function NewPasswordFields({
  idPrefix,
  password,
  confirm,
  onPasswordChange,
  onConfirmChange,
}: NewPasswordFieldsProps) {
  const checks = passwordChecks(password);
  return (
    <>
      <div className="field">
        <label htmlFor={`${idPrefix}-new`}>New password</label>
        <input
          id={`${idPrefix}-new`}
          type="password"
          value={password}
          onChange={(e) => onPasswordChange(e.target.value)}
          autoComplete="new-password"
          required
        />
      </div>
      {password.length > 0 && (
        <ul className="password-rules">
          {checks.map((check) => (
            <li key={check.label} className={check.met ? "met" : "unmet"}>
              {check.met ? "✓" : "○"} {check.label}
            </li>
          ))}
        </ul>
      )}
      <div className="field">
        <label htmlFor={`${idPrefix}-confirm`}>Confirm new password</label>
        <input
          id={`${idPrefix}-confirm`}
          type="password"
          value={confirm}
          onChange={(e) => onConfirmChange(e.target.value)}
          autoComplete="new-password"
          required
        />
      </div>
    </>
  );
}

/**
 * Returns the first problem with a new password and its confirmation, or null.
 * Exported for the admin reset form, which checks the same two fields.
 */
export function validateNewPassword(password: string, confirm: string): string | null {
  return (
    validatePassword(password) ??
    (password === confirm ? null : "The two new passwords don't match.")
  );
}

interface ChangePasswordFormProps {
  onSave: (currentPassword: string, newPassword: string) => Promise<void>;
  onCancel: () => void;
}

/**
 * Changes the signed-in user's own password. user-service ends every session
 * on success, this one included, so the parent signs the user out afterwards;
 * the copy says so before they submit.
 */
export function ChangePasswordForm({ onSave, onCancel }: ChangePasswordFormProps) {
  const [currentPassword, setCurrentPassword] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const { busy, error, setError, run } = useSave();

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!currentPassword) return setError("Enter your current password.");
    const problem = validateNewPassword(password, confirm);
    if (problem) return setError(problem);
    void run(() => onSave(currentPassword, password));
  };

  return (
    <>
      <h2 className="detail-title">Change password</h2>
      <form onSubmit={submit}>
        <div className="field">
          <label htmlFor="pw-current">Current password</label>
          <input
            id="pw-current"
            type="password"
            value={currentPassword}
            onChange={(e) => setCurrentPassword(e.target.value)}
            autoComplete="current-password"
            required
            autoFocus
          />
        </div>
        <NewPasswordFields
          idPrefix="pw"
          password={password}
          confirm={confirm}
          onPasswordChange={setPassword}
          onConfirmChange={setConfirm}
        />
        <p className="status-hint">
          Changing your password signs you out on every device, including this
          one. You'll log in again with the new password.
        </p>
        {error && <p className="status error">{error}</p>}
        <FormActions
          busy={busy}
          label="Change password"
          busyLabel="Changing…"
          onCancel={onCancel}
        />
      </form>
    </>
  );
}
