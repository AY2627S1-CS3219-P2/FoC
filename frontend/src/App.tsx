// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Application root — session gate, view switching, acting mode, and the
//   shared (mock) credit balance the shell displays. 2026-09-19: owns the
//   TokenStore and picks the fixture or gateway auth client (ai/decisions.md
//   D-010..D-015). 2026-09-21: logout sends both tokens, which
//   user-service's LogoutRequest requires. 2026-09-22: builds the authorized
//   transport and the suppliers client over it, now that suppliers go through
//   the gateway (D-010, D-025b).
// Author review: PENDING — <reviewer to complete>

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
import { createAuthorizedSend } from "./features/auth/session";
import { createSuppliersApi } from "./features/suppliers/suppliersApi";
import * as creditsApi from "./features/credits/creditsApi";
import { CreditsView } from "./features/credits/CreditsView";
import { MyErrandsView } from "./features/errands/MyErrandsView";
import { NewErrandView } from "./features/errands/NewErrandView";
import { HomeView } from "./features/home/HomeView";
import { ProfileView } from "./features/profile/ProfileView";
import { SuppliersView } from "./features/suppliers/SuppliersView";
import type { ViewName } from "./views";

/**
 * D-010 routes everything through the gateway, so a configured gateway URL is
 * what says "there is a real user-service behind this". Until one is set, the
 * fixtures run. Chosen once, here, rather than by a flag threaded into the
 * auth functions (root AGENTS.md §5, control coupling).
 */
const usingGateway = config.gatewayBaseUrl !== "";

const authClient = usingGateway
  ? {
      logIn: authApi.logInViaGateway,
      signUp: authApi.signUpViaGateway,
      // The gateway client reads the contact off the registration it already
      // holds, so the argument is accepted and ignored to keep one signature.
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
      logOut: (_accessToken: string, _refreshToken: string) => authApi.logOut(),
    };

export function App() {
  const [session, setSession] = useState<Session | null>(null);

  // Created once and never replaced. The tokens live in its closure, not in
  // component state, so a re-render cannot leak them into a React DevTools
  // tree and nothing outside lib/tokens.ts reads the values.
  const [tokens] = useState(createTokenStore);

  /**
   * The authorized transport, and the one client built over it.
   *
   * Created here because App owns the TokenStore, and passed down rather than
   * reached for: a module-level token would be exactly the global coupling
   * root AGENTS.md §5 rules out. One instance for the app's lifetime, so the
   * shared in-flight refresh inside it actually dedupes.
   *
   * onSessionExpired only clears local state — the refresh token is already
   * dead server-side, so there is nothing to revoke and no call to make.
   */
  const suppliers = useMemo(() => {
    const authorizedSend = createAuthorizedSend({
      tokens,
      refresh: authApi.refreshAccessTokenViaGateway,
      onSessionExpired: () => {
        tokens.clear();
        setSession(null);
        setView("home");
      },
    });
    return createSuppliersApi(authorizedSend);
  }, [tokens]);

  const [view, setView] = useState<ViewName>("home");
  const [mode, setMode] = useState<ActingMode>("requesting");
  const [isAdmin, setIsAdmin] = useState(false);
  const [toast, setToast] = useState<ToastMessage | null>(null);

  // Balance lives here because both the sidebar and the top bar show it, and
  // NewErrandView needs it for the "leaves you N available" hint. From the
  // mock credit-service until that service exists.
  const [available, setAvailable] = useState<number | null>(null);
  const [held, setHeld] = useState<number | null>(null);

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
      // The pair goes to the store; only the profile reaches component state.
      tokens.setPair(result.tokens);
      setSession(result.session);
    },
    [tokens],
  );

  /**
   * D-014: the server revokes — user-service deletes the refresh token and
   * blocklists the access token's jti. The client can only ask and then forget
   * its own copies, which it does either way.
   */
  const handleLogOut = useCallback(async () => {
    // Both, and before the store is cleared: user-service blocklists the
    // access token's jti and deletes the refresh token's session row, so it
    // needs each one. Its LogoutRequest makes refreshToken required.
    const accessToken = tokens.getAccessToken();
    const refreshToken = tokens.getRefreshToken();
    if (accessToken && refreshToken) {
      await authClient.logOut(accessToken, refreshToken);
    }
    tokens.clear();
    setSession(null);
    setView("home");
  }, [tokens]);

  if (!session) {
    return (
      <LoginPage
        onAuthenticated={handleAuthenticated}
        logIn={authClient.logIn}
        signUp={authClient.signUp}
        verifyRegistration={authClient.verifyRegistration}
        resendOtp={authClient.resendOtp}
        isMock={!usingGateway}
      />
    );
  }

  return (
    <>
      <AppShell
        session={session}
        view={view}
        onNavigate={setView}
        mode={mode}
        onModeChange={setMode}
        available={available}
        held={held}
        onLogOut={handleLogOut}
      >
        {view === "home" && <HomeView mode={mode} onNavigate={setView} />}

        {view === "new-errand" && (
          <NewErrandView
            suppliersApi={suppliers}
            available={available ?? 0}
            onNotify={setToast}
            onPosted={() => setView("my-errands")}
          />
        )}

        {view === "my-errands" && <MyErrandsView mode={mode} />}

        {view === "suppliers" && (
          <SuppliersView
            api={suppliers}
            isAdmin={isAdmin}
            onAdminChange={setIsAdmin}
            onNotify={setToast}
          />
        )}

        {view === "credits" && <CreditsView />}

        {view === "profile" && (
          <ProfileView
            session={session}
            isMock={!usingGateway}
            mode={mode}
            onModeChange={setMode}
            onLogOut={handleLogOut}
          />
        )}
      </AppShell>

      <Toast message={toast} onDismiss={dismissToast} />
    </>
  );
}
