// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Built the sign-in screen from the owner's mockup. 2026-09-19: takes
//   the auth calls as props so App picks fixture vs gateway (D-010).
//   2026-09-20: identifier login (F1.2.1), username and contact fields (F1.1,
//   F1.4.1), live password rules (F1.1.3.1), suspended accounts (F1.2.4), and
//   the OTP step (F1.1.2.7).
// Author review: PENDING — <reviewer to complete>

import { useState, type FormEvent } from "react";
import { MockBadge } from "../../components/MockBadge";
import { SuspendedAccountError, type AuthResult } from "./authApi";
import { OtpStep } from "./OtpStep";
import type { PendingRegistration } from "./types";
import {
  NUS_EMAIL_DOMAIN,
  passwordChecks,
  validateRegistration,
} from "./validation";

interface LoginPageProps {
  /**
   * Receives the session AND the token pair from D-011 — App puts the tokens
   * in the TokenStore. This screen never touches token storage itself.
   */
  onAuthenticated: (result: AuthResult) => void;
  /**
   * Injected by App, which chooses the fixture client or the gateway client
   * once at wiring time. Passing the functions beats passing a `useMock`
   * boolean the callee branches on (root AGENTS.md §5, control coupling).
   */
  logIn: (identifier: string, password: string) => Promise<AuthResult>;
  /** F1.1.2.7 — opens a registration; it is NOT complete until verified. */
  signUp: (
    email: string,
    username: string,
    password: string,
    contact: string,
  ) => Promise<PendingRegistration>;
  verifyRegistration: (
    pendingRegistration: PendingRegistration,
    code: string,
    contact: string,
  ) => Promise<AuthResult>;
  resendOtp: (
    pendingRegistration: PendingRegistration,
  ) => Promise<PendingRegistration>;
  /** Hides the mock notice once a real user-service is behind the gateway. */
  isMock: boolean;
}

type Tab = "login" | "signup";

/**
 * The initial credit allocation quoted below comes from the backlog's
 * configurable parameter X (100 credits at registration, F4.1). It is copy
 * here, not a rule — credit-service owns the number.
 */
export function LoginPage({
  onAuthenticated,
  logIn,
  signUp,
  verifyRegistration,
  resendOtp,
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
        setPending(await signUp(email, username, password, contact));
      }
    } catch (err) {
      // F1.2.4 — a suspended account is a distinct outcome, not a failed
      // credential check, and is told to the user as such.
      if (err instanceof SuspendedAccountError) setSuspended(true);
      setError(err instanceof Error ? err.message : "Could not sign in.");
    } finally {
      setBusy(false);
    }
  };

  if (pending) {
    return (
      <OtpStep
        pendingRegistration={pending}
        verify={verifyRegistration}
        resend={resendOtp}
        contact={contact}
        onVerified={onAuthenticated}
        onChanged={setPending}
        onCancel={() => setPending(null)}
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
