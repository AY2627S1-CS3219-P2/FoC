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
//   remembers which view the tab was on across that reload. 2026-10-10
//   (Claude Code, Opus 5.5): wires the gateway client's real OTP calls,
//   including resumeVerification for unverified logins.
//   2026-10-11 (Claude Code, Opus 5.5): one authorized transport now backs
//   both the suppliers and the user-service profile client; profile edits
//   update the session, a password change signs the user out, and the
//   admin view renders for ADMIN sessions only.
// Author review: Nigeltzy - Used to create the original boilerplate generation,
// afterall most react App.tsx files follow a similar pattern, and then afterwards it was used to update the changes based on my updated requirements, seems to have validly made 
// updates thereafter according to updating requirements and changes to other files.
// This file is the main entry point and so I typically review it every testing cycle as well.

// AI-generated (edited by James Teo): useRef, for the signed-in flag below.
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { AppShell } from "./components/AppShell";
import { Toast, type ToastMessage } from "./components/Toast";
import * as authApi from "./features/auth/authApi";
import type { AuthResult } from "./features/auth/authApi";
import { LoginPage } from "./features/auth/LoginPage";
import type { ActingMode, Session } from "./features/auth/types";
import { config } from "./lib/config";
import { createTokenStore } from "./lib/tokens";
// AI-generated (edited by James Teo): the remembered acting mode, and polling.
import {
  clearLastView,
  readLastMode,
  readLastView,
  writeLastMode,
  writeLastView,
} from "./lib/lastView";
import { usePolling } from "./lib/polling";
import { createAuthorizedSend } from "./features/auth/session";
import { createSuppliersApi } from "./features/suppliers/suppliersApi";
import { createSuppliersFixture } from "./features/suppliers/suppliersFixture";
// AI-generated (edited by James Teo): the order-service and credit-service
// clients replace the mocks, and NeedsService stands in for their screens in
// a fixture build.
import { NeedsService } from "./components/NeedsService";
import { createCreditAccountsApi } from "./features/credits/creditAccountsApi";
import { createOrdersApi } from "./features/errands/ordersApi";
import { balanceFigures, CreditsView, type Balance } from "./features/credits/CreditsView";
import { AllErrandsView } from "./features/errands/AllErrandsView";
import { FindErrandsView } from "./features/errands/FindErrandsView";
import { MyErrandsView } from "./features/errands/MyErrandsView";
import { NewErrandView } from "./features/errands/NewErrandView";
import { HomeView } from "./features/home/HomeView";
import { ProfileView } from "./features/profile/ProfileView";
import { createUsersApi, type UserProfile } from "./features/profile/usersApi";
import { AdminView } from "./features/admin/AdminView";
import { SuppliersView } from "./features/suppliers/SuppliersView";
// AI-generated (edited by James Teo): one availability rule for nav and guard.
import { isViewAvailable, type ViewName } from "./views";

/**
 * True unless the build sets VITE_USE_FIXTURES=true, in which case the auth
 * flow runs against in-browser fixtures instead of the gateway.
 */
const usingGateway = !config.useFixtures;

const authClient = usingGateway
  ? {
      logIn: authApi.logInViaGateway,
      signUp: authApi.signUpViaGateway,
      verifyRegistration: authApi.verifyRegistrationViaGateway,
      resendOtp: authApi.resendOtpViaGateway,
      resumeVerification: authApi.resumeVerificationViaGateway,
      logOut: authApi.logOutViaGateway,
    }
  : {
      logIn: (identifier: string, _password: string) => authApi.logIn(identifier),
      signUp: authApi.signUp,
      verifyRegistration: authApi.verifyRegistration,
      resendOtp: authApi.resendOtp,
      resumeVerification: authApi.resumeVerification,
      logOut: (_accessToken: string | null) => authApi.logOut(),
    };

export function App() {
  const [session, setSession] = useState<Session | null>(null);

  // AI-generated (edited by James Teo): true when the app ended the session
  // itself, so the login page says why it is showing. onSessionExpired is
  // made once and reads signedIn, so a refresh that fails after the user
  // logged out or changed their password sets nothing.
  const [sessionEnded, setSessionEnded] = useState(false);
  const signedIn = useRef(false);
  useEffect(() => {
    signedIn.current = session !== null;
  }, [session]);

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
  const { suppliers, usersApi, orders, creditAccounts } = useMemo(() => {
    // AI-generated (edited by nigeltzy).
    // A fixture build has no real token to send, so suppliers come from the
    // fixture too rather than from the gateway.
    // AI-generated (edited by nigeltzy): and there is no user-service to edit a
    // profile in, so usersApi is null and the edit controls stay hidden.
    // AI-generated (edited by James Teo): nor an order-service or credit-service
    // fixture, so those clients are null and their screens show NeedsService.
    if (!usingGateway) {
      return {
        suppliers: createSuppliersFixture(),
        usersApi: null,
        orders: null,
        creditAccounts: null,
      };
    }
    const authorizedSend = createAuthorizedSend({
      tokens,
      refresh: authApi.refreshAccessTokenViaGateway,
      onSessionExpired: () => {
        tokens.clear();
        // AI-generated (edited by James Teo): the next account on this tab
        // starts clean, as after a logout: no remembered view or mode, no
        // balance, no account handed to the Admin screen.
        clearLastView();
        setMode("requesting");
        setBalance({ status: "loading" });
        setAdminLookupId(null);
        // AI-generated (edited by James Teo): the login page says the user was
        // signed out, unless the session had already ended another way.
        if (signedIn.current) setSessionEnded(true);
        setSession(null);
        setView("home");
      },
    });
    return {
      suppliers: createSuppliersApi(authorizedSend),
      usersApi: createUsersApi(authorizedSend),
      // AI-generated (edited by James Teo).
      orders: createOrdersApi(authorizedSend),
      creditAccounts: createCreditAccountsApi(authorizedSend),
    };
  }, [tokens]);

  // Restored from sessionStorage so a reload lands where the tab was, not on
  // Home. Read once, at initial state: doing it in an effect would render Home
  // first and then jump.
  const [view, setView] = useState<ViewName>(() => readLastView() ?? "home");
  // AI-generated (edited by James Teo): the acting mode is restored the same
  // way, so a reload on a courier screen stays in Delivering mode.
  const [mode, setMode] = useState<ActingMode>(() => readLastMode() ?? "requesting");
  const [toast, setToast] = useState<ToastMessage | null>(null);

  // Balance lives here because the sidebar, the top bar and NewErrandView all
  // show it.
  // AI-generated (edited by James Teo): it comes from credit-service
  // (creditAccountsApi.ts), and the Credits screen shows this same copy. The
  // shell shows "—" unless it is "ready": while loading, with no account yet,
  // when credit-service fails, for an admin and in a fixture build.
  const [balance, setBalance] = useState<Balance>({ status: "loading" });

  // AI-generated (edited by James Teo): an account ID handed from an errand's
  // detail to the Admin screen, which looks it up on opening.
  const [adminLookupId, setAdminLookupId] = useState<string | null>(null);

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

  // AI-generated (edited by James Teo).
  useEffect(() => {
    if (session) writeLastMode(mode);
  }, [session, mode]);

  // AI-generated (edited by James Teo): credit-service's accounts/me, polled
  // every 10 s for a STUDENT session while the tab is visible, and refreshed
  // at once after the user's own errand actions. An ADMIN session has no
  // credit account and polls nothing. "No account yet" keeps polling, so a
  // new student's account appears without a reload.
  const refreshBalance = usePolling(
    session?.role === "STUDENT" && creditAccounts
      ? async (isActive) => {
          try {
            const account = await creditAccounts.getAccount();
            if (!isActive()) return;
            setBalance(account ? { status: "ready", account } : { status: "none" });
          } catch (err) {
            if (isActive()) setBalance({ status: "unavailable" });
            throw err;
          }
        }
      : null,
    10_000,
    [creditAccounts, session?.userId],
  );

  const dismissToast = useCallback(() => setToast(null), []);

  const handleAuthenticated = useCallback(
    (result: AuthResult) => {
      // The access token goes to the store; only the profile reaches component state.
      tokens.setPair(result.tokens);
      // AI-generated (edited by James Teo).
      setSessionEnded(false);
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
    // AI-generated (edited by James Teo): nothing of this session's carries
    // over to the next sign-in on this tab. The user logged out, so the
    // login page has nothing to explain, even if a poll's refresh failed
    // while the logout was on its way.
    setMode("requesting");
    setBalance({ status: "loading" });
    setAdminLookupId(null);
    setSessionEnded(false);
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
    // AI-generated (edited by James Teo): the toast below says why; a poll whose
    // refresh failed during the change must not add "signed out" to it.
    setMode("requesting");
    setBalance({ status: "loading" });
    setAdminLookupId(null);
    setSessionEnded(false);
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
          resumeVerification={authClient.resumeVerification}
          isMock={!usingGateway}
          // AI-generated (edited by James Teo).
          sessionEnded={sessionEnded}
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
  // AI-generated (edited by James Teo): widened to every view the session's
  // role and mode do not offer, by the rule the nav uses.
  const role = session.role;
  const shownView: ViewName = isViewAvailable(view, role, mode) ? view : "home";
  const { available, held } = balanceFigures(balance);

  // AI-generated (edited by James Teo): navigation, mode and the hand-over to
  // the Admin screen.
  /** A nav or in-page link: an account handed to the Admin screen is dropped. */
  const navigate = (next: ViewName) => {
    setAdminLookupId(null);
    setView(next);
  };

  /** Switching to a mode that does not offer the current view lands on Home. */
  const changeMode = (next: ActingMode) => {
    setMode(next);
    if (!isViewAvailable(view, role, next)) setView("home");
  };

  /** From an errand's detail: the Admin screen, with that account looked up. */
  const lookUpAccount = (uid: string) => {
    setAdminLookupId(uid);
    setView("admin");
  };

  /** After a post: the held credits show at once, then the new errand. */
  const handlePosted = () => {
    refreshBalance();
    navigate("my-errands");
  };

  return (
    <>
      {/* AI-generated (edited by James Teo): navigate, changeMode and the
          balance's figures. */}
      <AppShell
        session={session}
        view={shownView}
        onNavigate={navigate}
        mode={mode}
        onModeChange={changeMode}
        available={available}
        held={held}
        onLogOut={handleLogOut}
      >
        {/* AI-generated (edited by James Teo): Home's shortcuts follow the role. */}
        {shownView === "home" && <HomeView mode={mode} role={role} onNavigate={navigate} />}

        {/* AI-generated (edited by James Teo): the errand and credit screens
            get their clients, or NeedsService when the build has none. My
            errands remounts on a mode switch, so it starts on Active with
            nothing of the other mode's left open. */}
        {shownView === "new-errand" &&
          (orders ? (
            <NewErrandView
              suppliersApi={suppliers}
              ordersApi={orders}
              available={available}
              onNotify={setToast}
              onPosted={handlePosted}
              onCancel={() => navigate("my-errands")}
              onOpenCredits={() => navigate("credits")}
            />
          ) : (
            <NeedsService title="Post an errand" service="order-service" />
          ))}

        {shownView === "find-errands" &&
          (orders ? (
            <FindErrandsView
              ordersApi={orders}
              suppliersApi={suppliers}
              usersApi={usersApi}
              userId={session.userId}
              available={available}
              onNotify={setToast}
              onCreditsChanged={refreshBalance}
            />
          ) : (
            <NeedsService title="Find errands" service="order-service" />
          ))}

        {shownView === "my-errands" &&
          (orders ? (
            <MyErrandsView
              key={mode}
              mode={mode}
              ordersApi={orders}
              suppliersApi={suppliers}
              usersApi={usersApi}
              userId={session.userId}
              available={available}
              onNotify={setToast}
              onCreditsChanged={refreshBalance}
              onNavigate={navigate}
            />
          ) : (
            <NeedsService title="My errands" service="order-service" />
          ))}

        {shownView === "all-errands" &&
          (orders ? (
            <AllErrandsView
              ordersApi={orders}
              suppliersApi={suppliers}
              usersApi={usersApi}
              userId={session.userId}
              onNotify={setToast}
              onLookUpAccount={lookUpAccount}
            />
          ) : (
            <NeedsService title="All errands" service="order-service" />
          ))}

        {shownView === "suppliers" && (
          <SuppliersView
            api={suppliers}
            isMock={!usingGateway}
            isAdmin={session.role === "ADMIN"}
            onNotify={setToast}
          />
        )}

        {/* AI-generated (edited by James Teo): Credits shows App's balance,
            so it and the shell always agree. */}
        {shownView === "credits" &&
          (creditAccounts ? (
            <CreditsView api={creditAccounts} balance={balance} onRetryBalance={refreshBalance} />
          ) : (
            <NeedsService title="Credits" service="credit-service" />
          ))}

        {shownView === "profile" && (
          <ProfileView
            session={session}
            isMock={!usingGateway}
            usersApi={usersApi}
            onProfileUpdated={handleProfileUpdated}
            onPasswordChanged={handlePasswordChanged}
            onNotify={setToast}
            mode={mode}
            onModeChange={changeMode}
            onLogOut={handleLogOut}
          />
        )}

        {/* AI-generated (edited by nigeltzy). */}
        {/* AI-generated (edited by James Teo): keyed by the handed-over account,
            so each hand-over opens with that account looked up. */}
        {shownView === "admin" && (
          <AdminView
            key={adminLookupId ?? ""}
            usersApi={usersApi}
            currentUserId={session.userId}
            initialAccountId={adminLookupId}
            onNotify={setToast}
          />
        )}
      </AppShell>

      <Toast message={toast} onDismiss={dismissToast} />
    </>
  );
}
