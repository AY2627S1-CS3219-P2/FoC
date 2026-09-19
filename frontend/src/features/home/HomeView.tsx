// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Restyled the landing view to the owner's design system and pointed
//   its cards at the new views.
// Author review: PENDING — <reviewer to complete>

import type { ActingMode } from "../auth/types";
import type { ViewName } from "../../views";

interface HomeViewProps {
  mode: ActingMode;
  onNavigate: (view: ViewName) => void;
}

interface Shortcut {
  view: ViewName;
  icon: string;
  title: string;
  desc: string;
  /** Which acting mode this belongs to, or both. */
  modes: ActingMode[];
}

const SHORTCUTS: Shortcut[] = [
  {
    view: "new-errand",
    icon: "➕",
    title: "Request an errand",
    desc: "Ask someone already heading that way to collect something",
    modes: ["requesting"],
  },
  {
    view: "my-errands",
    icon: "📦",
    title: "My errands",
    desc: "Track what you have posted and what you are delivering",
    modes: ["requesting", "delivering"],
  },
  {
    view: "suppliers",
    icon: "🏬",
    title: "Suppliers",
    desc: "Campus stores, facilities and pickup points",
    modes: ["requesting", "delivering"],
  },
  {
    view: "credits",
    icon: "🪙",
    title: "Credits",
    desc: "Your balance in the closed campus economy",
    modes: ["requesting", "delivering"],
  },
];

export function HomeView({ mode, onNavigate }: HomeViewProps) {
  const shortcuts = SHORTCUTS.filter((s) => s.modes.includes(mode));

  return (
    <section>
      <div className="hero">
        <h1>Friend on Campus</h1>
        <p className="hero-sub">
          {mode === "requesting"
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
            <span className="service-icon">{shortcut.icon}</span>
            <span className="service-title">{shortcut.title}</span>
            <span className="service-desc">{shortcut.desc}</span>
          </button>
        ))}

        <div className="service-card disabled">
          <span className="service-icon">🔔</span>
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
