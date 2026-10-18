// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Restyled the landing view to the owner's design system and pointed
//   its cards at the new views.
// Author review: Nigeltzy - Checked the boilerplate generated file. Mostly just formatting and our selected text.

// AI-generated (edited by James Teo): shortcuts follow the nav's rule, by role
// and mode, with Find errands for couriers and All errands and Admin for
// admins.
import type { AccountRole, ActingMode } from "../auth/types";
import { isViewAvailable, type ViewName } from "../../views";

interface HomeViewProps {
  mode: ActingMode;
  // AI-generated (edited by James Teo).
  /** Picks the shortcuts with the mode, as the nav does. */
  role: AccountRole;
  onNavigate: (view: ViewName) => void;
}

// AI-generated (edited by James Teo): which shortcuts show is isViewAvailable's
// to say, so `modes` is gone from here.
interface Shortcut {
  view: ViewName;
  icon: string;
  title: string;
  desc: string;
}

// AI-generated (edited by James Teo): in the nav's order, the admin's two
// screens first.
const SHORTCUTS: Shortcut[] = [
  {
    view: "all-errands",
    icon: "📋",
    title: "All errands",
    desc: "Every errand in progress, the longest-stalled first",
  },
  {
    view: "admin",
    icon: "🛡️",
    title: "Admin",
    desc: "Look up an account by its ID",
  },
  // AI-generated (edited by James Teo): "Post", keeping "request" for HTTP.
  {
    view: "new-errand",
    icon: "➕",
    title: "Post an errand",
    desc: "Ask someone already heading that way to collect something",
  },
  // AI-generated (edited by James Teo).
  {
    view: "find-errands",
    icon: "🔎",
    title: "Find errands",
    desc: "Open errands on your way. Accept one and earn its reward",
  },
  {
    view: "my-errands",
    icon: "📦",
    title: "My errands",
    desc: "Track what you have posted and what you are delivering",
  },
  {
    view: "suppliers",
    icon: "🏬",
    title: "Suppliers",
    desc: "Campus stores, facilities and pickup points",
  },
  {
    view: "credits",
    icon: "🪙",
    title: "Credits",
    desc: "Your balance in the closed campus economy",
  },
];

// AI-generated (edited by James Teo): role.
export function HomeView({ mode, role, onNavigate }: HomeViewProps) {
  const shortcuts = SHORTCUTS.filter((s) => isViewAvailable(s.view, role, mode));

  return (
    <section>
      <div className="hero">
        <h1>Friend on Campus</h1>
        {/* AI-generated (edited by James Teo): an admin's line. */}
        <p className="hero-sub">
          {role === "ADMIN"
            ? "Keep errands moving: settle the ones that have stalled, and look up the accounts behind them."
            : mode === "requesting"
              ? "Someone is already walking past. Ask them to collect it for you, and hold a few credits until it arrives."
              : "Pick up an errand on a walk you were making anyway, and earn the credits held against it."}
        </p>
      </div>

      <div className="service-grid">
        {shortcuts.map((shortcut) => (
          <button
            key={shortcut.view}
            className="service-card"
            onClick={() => onNavigate(shortcut.view)}
          >
            {/* AI-generated (edited by James Teo): the emoji is decoration, so
                screen readers skip it and read the title. */}
            <span className="service-icon" aria-hidden="true">
              {shortcut.icon}
            </span>
            <span className="service-title">{shortcut.title}</span>
            <span className="service-desc">{shortcut.desc}</span>
          </button>
        ))}

        <div className="service-card disabled">
          {/* AI-generated (edited by James Teo): decoration, as above. */}
          <span className="service-icon" aria-hidden="true">
            🔔
          </span>
          <span className="service-title">Notifications</span>
          <span className="service-desc">
            Updates when an errand is accepted or delivered
          </span>
          <span className="service-soon">Coming soon</span>
        </div>
      </div>
    </section>
  );
}
