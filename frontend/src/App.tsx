// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Application root — session gate, view switching, acting mode, and the
//   shared (mock) credit balance the shell displays.
// Author review: PENDING — <reviewer to complete>

import { useCallback, useEffect, useState } from "react";
import { AppShell } from "./components/AppShell";
import { Toast, type ToastMessage } from "./components/Toast";
import { LoginPage } from "./features/auth/LoginPage";
import type { ActingMode, Session } from "./features/auth/types";
import * as creditsApi from "./features/credits/creditsApi";
import { CreditsView } from "./features/credits/CreditsView";
import { MyErrandsView } from "./features/errands/MyErrandsView";
import { NewErrandView } from "./features/errands/NewErrandView";
import { HomeView } from "./features/home/HomeView";
import { ProfileView } from "./features/profile/ProfileView";
import { SuppliersView } from "./features/suppliers/SuppliersView";
import type { ViewName } from "./views";

export function App() {
  const [session, setSession] = useState<Session | null>(null);
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

  if (!session) {
    return <LoginPage onAuthenticated={setSession} />;
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
        onLogOut={() => {
          setSession(null);
          setView("home");
        }}
      >
        {view === "home" && <HomeView mode={mode} onNavigate={setView} />}

        {view === "new-errand" && (
          <NewErrandView
            available={available ?? 0}
            onNotify={setToast}
            onPosted={() => setView("my-errands")}
          />
        )}

        {view === "my-errands" && <MyErrandsView mode={mode} />}

        {view === "suppliers" && (
          <SuppliersView
            isAdmin={isAdmin}
            onAdminChange={setIsAdmin}
            onNotify={setToast}
          />
        )}

        {view === "credits" && <CreditsView />}

        {view === "profile" && (
          <ProfileView
            session={session}
            mode={mode}
            onModeChange={setMode}
            onLogOut={() => {
              setSession(null);
              setView("home");
            }}
          />
        )}
      </AppShell>

      <Toast message={toast} onDismiss={dismissToast} />
    </>
  );
}
