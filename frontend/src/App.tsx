// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Application root — session gate, view switching, acting mode, and the
//   shared (mock) credit balance the shell displays. 2026-09-19: owns the
//   TokenStore and picks the fixture or gateway auth client (ai/decisions.md
//   D-010..D-015). 2026-09-21: logout sends both tokens, which
//   user-service's LogoutRequest requires. 2026-09-22: builds the authorized
//   transport and the suppliers client over it, now that suppliers go through
//   the gateway (D-010, D-025b). Then: restores the session from the refresh
//   cookie on mount, which is what makes a page reload survive (D-033), and
//   remembers which view the tab was on across that reload.
//   2026-10-11 (Claude Code, Opus 5.5): one authorized transport now backs
//   both the suppliers and the user-service profile client; profile edits
//   update the session, a password change signs the user out, and the
//   admin view renders for ADMIN sessions only.
// Author review: Nigeltzy - Used to create the original boilerplate generation,
// afterall most react App.tsx files follow a similar pattern, and then afterwards it was used to update the changes based on my updated requirements, seems to have validly made 
// updates thereafter according to updating requirements and changes to other files.
// This file is the main entry point and so I typically review it every testing cycle as well.

import { useCallback, useEffect, useMemo, useState } from "react";
import { AppShell } from "./components/AppShell";
import { Toast, type ToastMessage } from "./components/Toast";
import * as authApi from "./features/auth/authApi";
import type { AuthResult } from "./features/auth/authApi";
import { LoginPage } from "./features/auth/LoginPage";
import type {
  ActingMode,
  PendingRegistration,
  Session,
} from "./features/auth/types";
import { config } from "./lib/config";
import { createTokenStore } from "./lib/tokens";
import { clearLastView, readLastView, writeLastView } from "./lib/lastView";
import { createAuthorizedSend } from "./features/auth/session";
import { createSuppliersApi } from "./features/suppliers/suppliersApi";
import { createSuppliersFixture } from "./features/suppliers/suppliersFixture";
import * as creditsApi from "./features/credits/creditsApi";
import { CreditsView } from "./features/credits/CreditsView";
import { MyErrandsView } from "./features/errands/MyErrandsView";
import { NewErrandView } from "./features/errands/NewErrandView";
import { HomeView } from "./features/home/HomeView";
import { ProfileView } from "./features/profile/ProfileView";
import { createUsersApi, type UserProfile } from "./features/profile/usersApi";
import { AdminView } from "./features/admin/AdminView";
import { SuppliersView } from "./features/suppliers/SuppliersView";
import type { ViewName } from "./views";

/**
 * True unless the build sets VITE_USE_FIXTURES=true, in which case the auth
 * flow runs against in-browser fixtures instead of the gateway.
 */
const usingGateway = !config.useFixtures;

const authClient = usingGateway
  ? {
      logIn: authApi.logInViaGateway,
      signUp: authApi.signUpViaGateway,
      // The gateway client takes no contact, so the argument is accepted and
      // ignored to keep one signature for both clients.
      verifyRegistration: (
        pendingRegistration: PendingRegistration,
        code: string,
        _contact: string,
      ) => authApi.verifyRegistrationViaGateway(pendingRegistration, code),
      resendOtp: authApi.resendOtpViaGateway,
      logOut: authApi.logOutViaGateway,
    }
  : {
      logIn: (identifier: string, _password: string) => authApi.logIn(identifier),
      signUp: authApi.signUp,
      verifyRegistration: authApi.verifyRegistration,
      resendOtp: authApi.resendOtp,
      logOut: (_accessToken: string | null) => authApi.logOut(),
    };

export function App() {
  const [session, setSession] = useState<Session | null>(null);

  // True until the refresh cookie has been given its chance. Without this the
  // login page renders for a frame and then vanishes, which reads as a bug
  // even when the restore succeeds.
  const [restoring, setRestoring] = useState(usingGateway);

  // Created once and never replaced. The access token lives in the store's
  // closure, not in component state.
  const [tokens] = useState(createTokenStore);

  /**
   * The authorized transport and the clients built over it, made once per
   * TokenStore so concurrent requests share one in-flight refresh.
   *
   * onSessionExpired runs when a refresh fails for any reason and clears only
   * local state.
   */
  const { suppliers, usersApi } = useMemo(() => {
    // AI-generated (edited by nigeltzy).
    // A fixture build has no real token to send, so suppliers come from the
    // fixture too rather than from the gateway.
    // AI-generated (edited by nigeltzy): and there is no user-service to edit a
    // profile in, so usersApi is null and the edit controls stay hidden.
    if (!usingGateway) return { suppliers: createSuppliersFixture(), usersApi: null };
    const authorizedSend = createAuthorizedSend({
      tokens,
      refresh: authApi.refreshAccessTokenViaGateway,
      onSessionExpired: () => {
        tokens.clear();
        setSession(null);
        setView("home");
      },
    });
    return {
      suppliers: createSuppliersApi(authorizedSend),
      usersApi: createUsersApi(authorizedSend),
    };
  }, [tokens]);

  // Restored from sessionStorage so a reload lands where the tab was, not on
  // Home. Read once, at initial state: doing it in an effect would render Home
  // first and then jump.
  const [view, setView] = useState<ViewName>(() => readLastView() ?? "home");
  const [mode, setMode] = useState<ActingMode>("requesting");
  const [toast, setToast] = useState<ToastMessage | null>(null);

  // Balance lives here because the sidebar, the top bar and NewErrandView all
  // show it. It comes from the credit-service fixture (creditsApi.ts).
  const [available, setAvailable] = useState<number | null>(null);
  const [held, setHeld] = useState<number | null>(null);

  /**
   * A reload drops the in-memory access token but keeps the HttpOnly refresh
   * cookie. On mount, exchange the cookie for a new access token and profile;
   * with no live cookie the exchange fails and the login page shows.
   */
  useEffect(() => {
    if (!usingGateway) return;
    let cancelled = false;

    authApi
      .restoreSessionViaGateway()
      .then((restored) => {
        if (cancelled || !restored) return;
        tokens.setPair(restored.tokens);
        setSession(restored.session);
      })
      .finally(() => {
        if (!cancelled) setRestoring(false);
      });

    return () => {
      cancelled = true;
    };
  }, [tokens]);

  // Remember the view for this tab. Only while signed in: a signed-out tab
  // that remembered "profile" would just bounce off the login gate.
  useEffect(() => {
    if (session) writeLastView(view);
  }, [session, view]);

  useEffect(() => {
    if (!session) return;
    let cancelled = false;
    creditsApi.getBalance().then((balance) => {
      if (cancelled) return;
      setAvailable(balance.available);
      setHeld(balance.held);
    });
    return () => {
      cancelled = true;
    };
  }, [session]);

  const dismissToast = useCallback(() => setToast(null), []);

  const handleAuthenticated = useCallback(
    (result: AuthResult) => {
      // The access token goes to the store; only the profile reaches component state.
      tokens.setPair(result.tokens);
      setSession(result.session);
    },
    [tokens],
  );

  /**
   * Sends the logout when an access token is in memory, then clears the token,
   * the remembered view and the session whatever the answer.
   */
  const handleLogOut = useCallback(async () => {
    // Read before the store is cleared. The gateway adds the refresh token
    // from its cookie, so only the access token is sent.
    // AI-generated (edited by nigeltzy): called even with no token in memory,
    // since the gateway client refreshes before logging out.
    await authClient.logOut(tokens.getAccessToken());
    tokens.clear();
    clearLastView();
    setSession(null);
    setView("home");
  }, [tokens]);

  // AI-generated (edited by nigeltzy).
  /** A username or contact change: the saved values replace the session's. */
  const handleProfileUpdated = useCallback((profile: UserProfile) => {
    setSession((current) =>
      current
        ? {
            ...current,
            username: profile.username || current.username,
            contact: profile.contact,
            initials: authApi.initialsOf(profile.username || current.username),
          }
        : current,
    );
  }, []);

  // AI-generated (edited by nigeltzy).
  /**
   * user-service revokes every session on a password change, this one
   * included, so the refresh cookie is already dead. Clear local state and
   * send the user to the login page; no logout call, since the gateway would
   * only find a revoked session to end. Logging back in automatically is not
   * attempted: a login in the same second as the change can be issued a
   * refresh token user-service then rejects (its iat is whole seconds,
   * tokens_valid_after is not).
   */
  const handlePasswordChanged = useCallback(() => {
    tokens.clear();
    clearLastView();
    setSession(null);
    setView("home");
    setToast({ text: "Password changed. Log in with your new password.", kind: "success" });
  }, [tokens]);

  if (restoring) {
    return <div className="app-loading" aria-busy="true" />;
  }

  if (!session) {
    return (
      <>
        <LoginPage
          onAuthenticated={handleAuthenticated}
          logIn={authClient.logIn}
          signUp={authClient.signUp}
          verifyRegistration={authClient.verifyRegistration}
          resendOtp={authClient.resendOtp}
          isMock={!usingGateway}
        />
        {/* AI-generated (edited by nigeltzy): carries the password-changed
            notice onto the login page. */}
        <Toast message={toast} onDismiss={dismissToast} />
      </>
    );
  }

  // AI-generated (edited by nigeltzy).
  // A remembered "admin" view can outlive the account's ADMIN role, so a
  // non-admin session falls back to Home rather than rendering it.
  const shownView: ViewName = view === "admin" && session.role !== "ADMIN" ? "home" : view;

  return (
    <>
      <AppShell
        session={session}
        view={shownView}
        onNavigate={setView}
        mode={mode}
        onModeChange={setMode}
        available={available}
        held={held}
        onLogOut={handleLogOut}
      >
        {shownView === "home" && <HomeView mode={mode} onNavigate={setView} />}

        {shownView === "new-errand" && (
          <NewErrandView
            suppliersApi={suppliers}
            suppliersMock={!usingGateway}
            available={available ?? 0}
            onNotify={setToast}
            onPosted={() => setView("my-errands")}
          />
        )}

        {shownView === "my-errands" && <MyErrandsView mode={mode} />}

        {shownView === "suppliers" && (
          <SuppliersView
            api={suppliers}
            isMock={!usingGateway}
            isAdmin={session.role === "ADMIN"}
            onNotify={setToast}
          />
        )}

        {shownView === "credits" && <CreditsView />}

        {shownView === "profile" && (
          <ProfileView
            session={session}
            isMock={!usingGateway}
            usersApi={usersApi}
            onProfileUpdated={handleProfileUpdated}
            onPasswordChanged={handlePasswordChanged}
            onNotify={setToast}
            mode={mode}
            onModeChange={setMode}
            onLogOut={handleLogOut}
          />
        )}

        {/* AI-generated (edited by nigeltzy). */}
        {shownView === "admin" && (
          <AdminView usersApi={usersApi} currentUserId={session.userId} onNotify={setToast} />
        )}
      </AppShell>

      <Toast message={toast} onDismiss={dismissToast} />
    </>
  );
}
