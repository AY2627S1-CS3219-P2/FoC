// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Built the application chrome from the owner's mockups — navy sidebar
//   on desktop, bottom tab bar on mobile, top bar with avatar, acting-mode
//   toggle and credits pill.
// Author review: Nigeltzy - AI created the implementation as per the mockup that the team designed.
// Code wise and UI looks wise, seems valid and as intended.
//
// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5), date: 2026-10-02
// Scope: Mobile nav is now an off-canvas drawer instead of the bottom tab
//   bar: hidden below 860px by default, opened by a new hamburger button in
//   the top bar, closed by tapping its backdrop or any nav item. Removed the
//   dead tab-bar JSX.
// Author review: nigeltzy — Checked that the component was updated according to the new format and requirements. All seems valid when checked.

// AI-generated (edited by James Teo): the mock badge is gone now that the
// balance is credit-service's, and the nav follows isViewAvailable.
import { useState, type ReactNode } from "react";
import type { ActingMode, Session } from "../features/auth/types";
import { isViewAvailable, NAV_ITEMS, type ViewName } from "../views";
import {
  BellIcon,
  HomeIcon,
  ListIcon,
  LogoutIcon,
  MenuIcon,
  PlusIcon,
  SearchIcon,
  ShieldIcon,
  StoreIcon,
  UserIcon,
  WalletIcon,
} from "./icons";

// AI-generated (edited by James Teo): Find errands and All errands reuse the
// search and list icons.
const ICONS: Record<ViewName, (p: { size?: number }) => ReactNode> = {
  home: HomeIcon,
  "new-errand": PlusIcon,
  "find-errands": SearchIcon,
  "my-errands": ListIcon,
  suppliers: StoreIcon,
  credits: WalletIcon,
  profile: UserIcon,
  notifications: BellIcon,
  admin: ShieldIcon,
  "all-errands": ListIcon,
};

interface AppShellProps {
  session: Session;
  view: ViewName;
  onNavigate: (view: ViewName) => void;
  mode: ActingMode;
  onModeChange: (mode: ActingMode) => void;
  // AI-generated (edited by James Teo).
  /** Null whenever credit-service's balance is not known; shows "—". */
  available: number | null;
  held: number | null;
  onLogOut: () => void;
  children: ReactNode;
}

export function AppShell({
  session,
  view,
  onNavigate,
  mode,
  onModeChange,
  available,
  held,
  onLogOut,
  children,
}: AppShellProps) {
  const [mobileNavOpen, setMobileNavOpen] = useState(false);
  // AI-generated (edited by James Teo): an admin never posts or carries an
  // errand and has no credit account, so gets no mode toggle and no balance.
  const isStudent = session.role === "STUDENT";

  const navigateAndClose = (next: ViewName) => {
    onNavigate(next);
    setMobileNavOpen(false);
  };

  return (
    <div className="app-shell">
      {mobileNavOpen && (
        <button
          className="sidebar-backdrop"
          aria-label="Close menu"
          onClick={() => setMobileNavOpen(false)}
        />
      )}

      <aside className={`sidebar ${mobileNavOpen ? "open" : ""}`}>
        <button className="sidebar-brand" onClick={() => navigateAndClose("home")}>
          <span className="brand-mark">FoC</span>
          <span>Friend on Campus</span>
        </button>

        <nav className="sidebar-nav">
          {/* AI-generated (edited by nigeltzy): admin-only items are left out
              of a non-admin session's nav. */}
          {/* AI-generated (edited by James Teo): and each role and mode gets
              only its own items; the current one is marked for screen
              readers too. */}
          {NAV_ITEMS.filter((item) => isViewAvailable(item.view, session.role, mode)).map((item) => {
            const Icon = ICONS[item.view];
            return (
              <button
                key={item.view}
                className={`sidebar-link ${view === item.view ? "active" : ""}`}
                aria-current={view === item.view ? "page" : undefined}
                onClick={() => navigateAndClose(item.view)}
                disabled={!item.enabled}
                title={item.enabled ? undefined : "Coming soon"}
              >
                <Icon />
                <span>{item.label}</span>
              </button>
            );
          })}
        </nav>

        <div className="sidebar-foot">
          {/* AI-generated (edited by James Teo): credit-service's balance, for a
              student only, with no mock badge. */}
          {isStudent && (
            <div className="sidebar-balance">
              <div className="sidebar-balance-value">
                {available === null ? "—" : available}
              </div>
              <div className="sidebar-balance-label">credits available</div>
              {held !== null && held > 0 && (
                <div className="sidebar-balance-held">
                  {held} held on errands in progress
                </div>
              )}
            </div>
          )}
          <button className="sidebar-link" onClick={onLogOut}>
            <LogoutIcon />
            <span>Log out</span>
          </button>
        </div>
      </aside>

      <div className="main">
        <header className="topbar">
          <button
            className="menu-toggle"
            aria-label="Open menu"
            onClick={() => setMobileNavOpen(true)}
          >
            <MenuIcon />
          </button>

          <span className="topbar-user">
            <span className="avatar">{session.initials}</span>
            <span>{session.username}</span>
          </span>

          <span className="topbar-spacer" />

          {/* AI-generated (edited by James Teo): a student's mode toggle, with
              the chosen mode announced. */}
          {isStudent && (
            <div className="mode-toggle" role="group" aria-label="Acting as">
              <button
                className={mode === "requesting" ? "active" : ""}
                aria-pressed={mode === "requesting"}
                onClick={() => onModeChange("requesting")}
              >
                Requesting
              </button>
              <button
                className={mode === "delivering" ? "active" : ""}
                aria-pressed={mode === "delivering"}
                onClick={() => onModeChange("delivering")}
              >
                Delivering
              </button>
            </div>
          )}

          {/* AI-generated (edited by nigeltzy). */}
          {/* AI-generated (edited by James Teo): a student's balance with no
              mock label, read out as "N credits available" rather than with
              the short "cr" shown. */}
          {isStudent && (
            <span className="credit-pill" title="Credits available">
              <strong>{available === null ? "—" : available}</strong>
              <span aria-hidden="true">cr</span>
              <span className="visually-hidden"> credits available</span>
            </span>
          )}
        </header>

        <main className="content">{children}</main>
      </div>
    </div>
  );
}
