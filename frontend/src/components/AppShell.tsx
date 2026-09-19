// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Built the application chrome from the owner's mockups — navy sidebar
//   on desktop, bottom tab bar on mobile, top bar with avatar, acting-mode
//   toggle and credits pill.
// Author review: PENDING — <reviewer to complete>

import type { ReactNode } from "react";
import type { ActingMode, Session } from "../features/auth/types";
import { NAV_ITEMS, type ViewName } from "../views";
import {
  BellIcon,
  HomeIcon,
  ListIcon,
  LogoutIcon,
  PlusIcon,
  StoreIcon,
  UserIcon,
  WalletIcon,
} from "./icons";

const ICONS: Record<ViewName, (p: { size?: number }) => ReactNode> = {
  home: HomeIcon,
  "new-errand": PlusIcon,
  "my-errands": ListIcon,
  suppliers: StoreIcon,
  credits: WalletIcon,
  profile: UserIcon,
  notifications: BellIcon,
};

interface AppShellProps {
  session: Session;
  view: ViewName;
  onNavigate: (view: ViewName) => void;
  mode: ActingMode;
  onModeChange: (mode: ActingMode) => void;
  /** Null while the (mock) credit balance is still loading. */
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
  const tabItems = NAV_ITEMS.filter((item) => item.inTabBar);

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <button className="sidebar-brand" onClick={() => onNavigate("home")}>
          <span className="brand-mark">FoC</span>
          <span>Friend on Campus</span>
        </button>

        <nav className="sidebar-nav">
          {NAV_ITEMS.map((item) => {
            const Icon = ICONS[item.view];
            return (
              <button
                key={item.view}
                className={`sidebar-link ${view === item.view ? "active" : ""}`}
                onClick={() => onNavigate(item.view)}
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
          <div className="sidebar-balance">
            <div className="sidebar-balance-value">
              {available === null ? "—" : available}
            </div>
            <div className="sidebar-balance-label">credits available</div>
            {held !== null && held > 0 && (
              <div className="sidebar-balance-held">
                {held} held on open errands
              </div>
            )}
          </div>
          <button className="sidebar-link" onClick={onLogOut}>
            <LogoutIcon />
            <span>Log out</span>
          </button>
        </div>
      </aside>

      <div className="main">
        <header className="topbar">
          <span className="topbar-user">
            <span className="avatar">{session.initials}</span>
            <span>{session.name}</span>
          </span>

          <span className="topbar-spacer" />

          <div className="mode-toggle">
            <button
              className={mode === "requesting" ? "active" : ""}
              onClick={() => onModeChange("requesting")}
            >
              Requesting
            </button>
            <button
              className={mode === "delivering" ? "active" : ""}
              onClick={() => onModeChange("delivering")}
            >
              Delivering
            </button>
          </div>

          <span className="credit-pill">
            <strong>{available === null ? "—" : available}</strong>
            <span>cr</span>
          </span>
        </header>

        <main className="content">{children}</main>
      </div>

      <nav className="tabbar">
        {tabItems.map((item) => {
          const Icon = ICONS[item.view];
          return (
            <button
              key={item.view}
              className={view === item.view ? "active" : ""}
              onClick={() => onNavigate(item.view)}
              disabled={!item.enabled}
            >
              <Icon size={19} />
              <span>{item.label}</span>
            </button>
          );
        })}
      </nav>
    </div>
  );
}
