// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Built the sign-in screen from the owner's mockup, against the mock
//   user-service.
// Author review: PENDING — <reviewer to complete>

import { useState, type FormEvent } from "react";
import { MockBadge } from "../../components/MockBadge";
import * as authApi from "./authApi";
import type { Session } from "./types";

interface LoginPageProps {
  onAuthenticated: (session: Session) => void;
}

type Tab = "login" | "signup";

/**
 * The initial credit allocation quoted below comes from the owner's mockup.
 * The actual figure is credit-service's to decide (its AGENTS.md calls the
 * size of the allocation the team's choice), so it is copy here, not a rule.
 */
export function LoginPage({ onAuthenticated }: LoginPageProps) {
  const [tab, setTab] = useState<Tab>("login");
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const session =
        tab === "login"
          ? await authApi.logIn(email)
          : await authApi.signUp(email, name);
      onAuthenticated(session);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not sign in.");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="auth-page">
      <div className="auth-card">
        <div className="brand-mark lg">FoC</div>
        <h1 className="auth-title">Friend on Campus</h1>
        <p className="auth-sub">Someone&rsquo;s already walking past. Ask them.</p>

        <MockBadge service="user-service" />

        <div className="tabs">
          <button
            className={tab === "login" ? "active" : ""}
            onClick={() => setTab("login")}
          >
            Log in
          </button>
          <button
            className={tab === "signup" ? "active" : ""}
            onClick={() => setTab("signup")}
          >
            Create account
          </button>
        </div>

        <form onSubmit={submit}>
          {tab === "signup" && (
            <div className="field">
              <label htmlFor="auth-name">Name</label>
              <input
                id="auth-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="Nigel Teo"
              />
            </div>
          )}

          <div className="field">
            <label htmlFor="auth-email">NUS email</label>
            <input
              id="auth-email"
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="nigel@u.nus.edu"
            />
          </div>

          <div className="field">
            <label htmlFor="auth-password">Password</label>
            <input
              id="auth-password"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="••••••••"
            />
          </div>

          {error && <p className="status error">{error}</p>}

          <button className="btn btn-primary btn-block" disabled={busy}>
            {busy ? "Signing in…" : tab === "login" ? "Log in" : "Create account"}
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
