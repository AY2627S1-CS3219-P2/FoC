// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-20
// Scope: The OTP verification step of registration (F1.1.2.3-F1.1.2.7).
// Author review: PENDING — <reviewer to complete>

import { useEffect, useState, type FormEvent } from "react";
import type { AuthResult } from "./authApi";
import type { PendingRegistration } from "./types";
import { OTP_LENGTH } from "./validation";

interface OtpStepProps {
  pendingRegistration: PendingRegistration;
  /** F1.1.2.7 — the only path that completes registration. */
  verify: (
    pendingRegistration: PendingRegistration,
    code: string,
    contact: string,
  ) => Promise<AuthResult>;
  /** F1.1.2.4 — request a replacement code. */
  resend: (
    pendingRegistration: PendingRegistration,
  ) => Promise<PendingRegistration>;
  /** Carried through from the signup form; stored on the new account. */
  contact: string;
  onVerified: (result: AuthResult) => void;
  onChanged: (pendingRegistration: PendingRegistration) => void;
  onCancel: () => void;
  /** True while the fixture client is in use, so the code can be shown. */
  isMock: boolean;
}

function useCountdown(target: number): number {
  const [remaining, setRemaining] = useState(() =>
    Math.max(0, target - Date.now()),
  );
  useEffect(() => {
    setRemaining(Math.max(0, target - Date.now()));
    const id = window.setInterval(() => {
      setRemaining(Math.max(0, target - Date.now()));
    }, 1000);
    return () => window.clearInterval(id);
  }, [target]);
  return remaining;
}

function formatDuration(ms: number): string {
  const total = Math.ceil(ms / 1000);
  const minutes = Math.floor(total / 60);
  const seconds = total % 60;
  return `${minutes}:${String(seconds).padStart(2, "0")}`;
}

/**
 * Step two of registration. F1.1.2.7 makes this mandatory: an account does not
 * exist until the correct code is submitted, so there is no way past this
 * screen other than verifying or abandoning the registration.
 */
export function OtpStep({
  pendingRegistration,
  verify,
  resend,
  contact,
  onVerified,
  onChanged,
  onCancel,
  isMock,
}: OtpStepProps) {
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  // F1.1.2.5 — the code expires five minutes after it was issued.
  const msLeft = useCountdown(pendingRegistration.expiresAt);
  const expired = msLeft <= 0;

  // F1.1.2.6 — after three replacements the block has its own countdown.
  const blockedMsLeft = useCountdown(pendingRegistration.blockedUntil ?? 0);
  const blocked =
    pendingRegistration.blockedUntil !== null && blockedMsLeft > 0;

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      onVerified(await verify(pendingRegistration, code, contact));
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not verify that code.");
    } finally {
      setBusy(false);
    }
  };

  const requestNewCode = async () => {
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      const next = await resend(pendingRegistration);
      onChanged(next);
      setCode("");
      setNotice("A new code is on its way. The previous one no longer works.");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not send a new code.");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="auth-page">
      <div className="auth-card">
        <div className="brand-mark lg">FoC</div>
        <h1 className="auth-title">Check your email</h1>
        <p className="auth-sub">
          We sent a {OTP_LENGTH}-digit code to <strong>{pendingRegistration.email}</strong>.
        </p>

        {isMock && (
          <p className="status status-hint">
            No mail server in development. The fixture&rsquo;s code is{" "}
            <strong>{pendingRegistration.fixtureCode}</strong>.
          </p>
        )}

        <form onSubmit={submit}>
          <div className="field">
            <label htmlFor="auth-otp">Verification code</label>
            <input
              id="auth-otp"
              value={code}
              onChange={(e) =>
                // Digits only, and never more than the code length, so the
                // field cannot hold something that is obviously not a code.
                setCode(e.target.value.replace(/[^0-9]/g, "").slice(0, OTP_LENGTH))
              }
              inputMode="numeric"
              autoComplete="one-time-code"
              placeholder="123456"
              disabled={busy || expired}
              autoFocus
            />
          </div>

          <p className="status-hint">
            {expired
              ? "That code has expired."
              : `This code expires in ${formatDuration(msLeft)}.`}
          </p>

          {error && <p className="status error">{error}</p>}
          {notice && !error && <p className="status">{notice}</p>}

          <button
            className="btn btn-primary btn-block"
            // F7.2.1 — the control is disabled while a submission is in flight.
            disabled={busy || expired || code.length !== OTP_LENGTH}
          >
            {busy ? "Verifying…" : "Verify and create account"}
          </button>
        </form>

        <div className="btn-row">
          <button
            type="button"
            className="btn btn-secondary"
            onClick={requestNewCode}
            disabled={busy || blocked}
          >
            {blocked
              ? `Blocked for ${formatDuration(blockedMsLeft)}`
              : "Send a new code"}
          </button>
          <button
            type="button"
            className="btn btn-secondary"
            onClick={onCancel}
            disabled={busy}
          >
            Back
          </button>
        </div>

        <p className="auth-note">
          {blocked
            ? "Too many code requests. You can ask again once the timer runs out."
            : `${pendingRegistration.resendsRemaining} replacement code${
                pendingRegistration.resendsRemaining === 1 ? "" : "s"
              } left. Your account isn't created until you enter the code.`}
        </p>
      </div>
    </div>
  );
}
