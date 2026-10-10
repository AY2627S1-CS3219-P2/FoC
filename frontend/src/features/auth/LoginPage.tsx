// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Built the sign-in screen from the owner's mockup. 2026-09-19: takes
//   the auth calls as props so App picks fixture vs gateway (D-010).
//   2026-09-20: identifier login (F1.2.1), username and contact fields (F1.1,
//   F1.4.1), live password rules (F1.1.3.1), suspended accounts (F1.2.4), and
//   the OTP step (F1.1.2.7). 2026-10-10 (Claude Code, Opus 5.5): verifying a
//   code now only activates the account, so this screen logs in afterwards
//   with the credentials it already holds; a login refused as unverified
//   reopens the OTP step when the identifier is an email.
// Author review: Nigeltzy - AI was used to create the implementation for the sign-in screen as per the mockup the team designed. Checked and is valid and works as intended.

import { useState, type FormEvent } from "react";
import { MockBadge } from "../../components/MockBadge";
import {
  SuspendedAccountError,
  UnverifiedAccountError,
  type AuthResult,
} from "./authApi";
import { OtpStep } from "./OtpStep";
import type { PendingRegistration } from "./types";
import {
  NUS_EMAIL_DOMAIN,
  passwordChecks,
  validateRegistration,
} from "./validation";

interface LoginPageProps {
  /**
   * Receives the session and the access token. App stores the token, so this
   * screen never touches token storage.
   */
  onAuthenticated: (result: AuthResult) => void;
  /**
   * Injected by App, which picks the fixture or gateway client once at
   * startup.
   */
  logIn: (identifier: string, password: string) => Promise<AuthResult>;
  /** F1.1.2.7: opens a registration, which is not complete until verified. */
  signUp: (
    email: string,
    username: string,
    password: string,
    contact: string,
  ) => Promise<PendingRegistration>;
  /** Activates the account on the correct code. Returns no session. */
  verifyRegistration: (
    pendingRegistration: PendingRegistration,
    code: string,
  ) => Promise<void>;
  resendOtp: (
    pendingRegistration: PendingRegistration,
  ) => Promise<PendingRegistration>;
  /**
   * Sends a fresh code to an account that registered but never verified, for
   * a login refused with UnverifiedAccountError.
   */
  resumeVerification: (email: string) => Promise<PendingRegistration>;
  /** True when the fixture client is in use; shows the mock notice. */
  isMock: boolean;
}

type Tab = "login" | "signup";

/**
 * Sign-in and registration screen. The 100-credit starting allocation it
 * quotes is the backlog's parameter X (F4.1); it is display copy, not a rule
 * this screen applies.
 */
export function LoginPage({
  onAuthenticated,
  logIn,
  signUp,
  verifyRegistration,
  resendOtp,
  resumeVerification,
  isMock,
}: LoginPageProps) {
  const [tab, setTab] = useState<Tab>("login");
  const [identifier, setIdentifier] = useState("");
  const [email, setEmail] = useState("");
  const [username, setUsername] = useState("");
  const [contact, setContact] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [suspended, setSuspended] = useState(false);

  // F1.1.2.7 — set once registration is open and awaiting its code. While
  // this is non-null the OTP step replaces the form entirely: there is no
  // route to an account that skips it.
  const [pending, setPending] = useState<PendingRegistration | null>(null);
  // AI-generated (edited by PENDING).
  // The identifier to log in with once the code is verified: the signup email,
  // or what was typed at a login refused as unverified. The password stays in
  // `password` state, which this screen already holds.
  const [verifiedLogin, setVerifiedLogin] = useState("");
  // True once user-service has accepted the code for `pending`.
  const [codeAccepted, setCodeAccepted] = useState(false);

  // AI-generated (edited by PENDING).
  // Every way into or out of the OTP step goes through here, so a new
  // registration never inherits the last one's accepted code.
  const openOtpStep = (next: PendingRegistration | null, loginWith = "") => {
    setVerifiedLogin(loginWith);
    setCodeAccepted(false);
    setPending(next);
  };

  const checks = passwordChecks(password);
  const passwordReady = checks.every((check) => check.met);

  const switchTab = (next: Tab) => {
    setTab(next);
    setError(null);
    setSuspended(false);
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    setSuspended(false);
    try {
      if (tab === "login") {
        onAuthenticated(await logIn(identifier, password));
      } else {
        // F1.1.1 — reject before a round trip when a field is missing, and
        // F1.1.2.1/F1.1.3.1/F1.1.4.2 for the format rules.
        const problem = validateRegistration({ email, username, password });
        if (problem) {
          setError(problem);
          return;
        }
        openOtpStep(await signUp(email, username, password, contact), email.trim());
      }
    } catch (err) {
      if (err instanceof UnverifiedAccountError) {
        await resumeUnverified();
        return;
      }
      // F1.2.4 — a suspended account is a distinct outcome, not a failed
      // credential check, and is told to the user as such.
      if (err instanceof SuspendedAccountError) setSuspended(true);
      setError(err instanceof Error ? err.message : "Could not sign in.");
    } finally {
      setBusy(false);
    }
  };

  // AI-generated (edited by PENDING).
  // F1.1.2.7 — the account exists but its email is unverified. A new code can
  // only be requested by email, so a username login is told to use the email.
  const resumeUnverified = async () => {
    const typed = identifier.trim();
    if (!typed.includes("@")) {
      setError(
        "This account's email isn't verified yet. Log in with your NUS email " +
          "to get a verification code.",
      );
      return;
    }
    try {
      openOtpStep(await resumeVerification(typed), typed);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not send a code.");
    }
  };

  // AI-generated (edited by PENDING).
  // Verifying returns no session, so log in straight after. user-service
  // deletes a code once it is used, so if the login fails a retry must not
  // verify again — it would be told the code expired — only log in.
  const verifyThenLogIn = async (
    pendingRegistration: PendingRegistration,
    code: string,
  ): Promise<AuthResult> => {
    if (!codeAccepted) {
      await verifyRegistration(pendingRegistration, code);
      setCodeAccepted(true);
    }
    return logIn(verifiedLogin, password);
  };

  if (pending) {
    return (
      <OtpStep
        pendingRegistration={pending}
        verify={verifyThenLogIn}
        resend={resendOtp}
        onVerified={onAuthenticated}
        onChanged={setPending}
        onCancel={() => openOtpStep(null)}
        isMock={isMock}
      />
    );
  }

  return (
    <div className="auth-page">
      <div className="auth-card">
        <div className="brand-mark lg">FoC</div>
        <h1 className="auth-title">Friend on Campus</h1>
        <p className="auth-sub">Someone&rsquo;s already walking past. Ask them.</p>

        {isMock && <MockBadge service="user-service" />}

        <div className="tabs">
          <button
            type="button"
            className={tab === "login" ? "active" : ""}
            onClick={() => switchTab("login")}
          >
            Log in
          </button>
          <button
            type="button"
            className={tab === "signup" ? "active" : ""}
            onClick={() => switchTab("signup")}
          >
            Create account
          </button>
        </div>

        <form onSubmit={submit}>
          {tab === "login" ? (
            <div className="field">
              {/* F1.2.1 — one field, username or NUS email. */}
              <label htmlFor="auth-identifier">Username or NUS email</label>
              <input
                id="auth-identifier"
                type="text"
                value={identifier}
                onChange={(e) => setIdentifier(e.target.value)}
                placeholder="nigeltzy"
                autoComplete="username"
                required
              />
            </div>
          ) : (
            <>
              <div className="field">
                <label htmlFor="auth-email">NUS email</label>
                <input
                  id="auth-email"
                  type="email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  placeholder={`nigel${NUS_EMAIL_DOMAIN}`}
                  autoComplete="email"
                  required
                />
              </div>

              <div className="field">
                <label htmlFor="auth-username">Username</label>
                <input
                  id="auth-username"
                  type="text"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  placeholder="nigeltzy"
                  autoComplete="username"
                  maxLength={128}
                  required
                />
                <p className="status-hint">Letters and numbers only.</p>
              </div>

              <div className="field">
                <label htmlFor="auth-contact">
                  Contact number <span className="field-optional">for couriers</span>
                </label>
                <input
                  id="auth-contact"
                  type="tel"
                  value={contact}
                  onChange={(e) => setContact(e.target.value)}
                  placeholder="9123 4567"
                  autoComplete="tel"
                />
              </div>
            </>
          )}

          <div className="field">
            <label htmlFor="auth-password">Password</label>
            <input
              id="auth-password"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="••••••••"
              autoComplete={tab === "login" ? "current-password" : "new-password"}
              required
            />
          </div>

          {/* F1.1.3.1 — shown live while signing up, so the user can act on
              it before submitting rather than after being rejected. */}
          {tab === "signup" && password.length > 0 && (
            <ul className="password-rules">
              {checks.map((check) => (
                <li key={check.label} className={check.met ? "met" : "unmet"}>
                  {check.met ? "✓" : "○"} {check.label}
                </li>
              ))}
            </ul>
          )}

          {error && (
            <p className={suspended ? "status error suspended" : "status error"}>
              {error}
            </p>
          )}

          <button
            className="btn btn-primary btn-block"
            // F7.2.1 — disabled while the first submission is in flight.
            disabled={busy || (tab === "signup" && !passwordReady)}
          >
            {busy
              ? tab === "login"
                ? "Signing in…"
                : "Sending code…"
              : tab === "login"
                ? "Log in"
                : "Create account"}
          </button>
        </form>

        <p className="auth-note">
          Sign up gives you 100 credits to start. Credits stay on the platform —
          they can&rsquo;t be bought, cashed out, or transferred off it.
        </p>
      </div>
    </div>
  );
}
