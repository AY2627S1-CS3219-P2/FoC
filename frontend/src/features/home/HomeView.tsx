// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Restyled the landing view to the owner's design system and pointed
//   its cards at the new views.
// Author review: Nigeltzy - Checked the boilerplate generated file. Mostly just formatting and our selected text.

// AI-generated (edited by James Teo): shortcuts follow the nav's rule, by role
// and mode, with Find errands for couriers and All errands and Admin for
// admins.
import { useState } from "react";
import { usePolling } from "../../lib/polling";
import { isViewAvailable, type ViewName } from "../../views";
import type { AccountRole, ActingMode } from "../auth/types";
import { stateBadgeClass } from "../errands/ErrandCard";
import { orderErrorMessage, STATE_LABEL } from "../errands/format";
import type { OrdersApi } from "../errands/ordersApi";
import type { Errand } from "../errands/orderTypes";

interface HomeViewProps {
  mode: ActingMode;
  // AI-generated (edited by James Teo).
  /** Picks the shortcuts with the mode, as the nav does. */
  role: AccountRole;
  onNavigate: (view: ViewName) => void;
  // AI-generated (edited by Claude Code, 2026-11-09): Home shows what is happening
  // right now (D-154), not only where to go.
  /** Null in a fixture build, which has no order-service to ask. */
  ordersApi?: OrdersApi | null;
  /** The signed-in student's own id, to leave their own open errands out of "waiting". */
  userId?: string;
  /** Null whenever credit-service's balance is not known. */
  available?: number | null;
  held?: number | null;
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
  // AI-generated (edited by Claude Code, 2026-11-03): one dashboard in place of
  // the two admin screens.
  {
    view: "admin",
    icon: "🛡️",
    title: "Dashboard",
    desc: "Errands in progress and past, accounts, and the admin audit log",
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
/** What a student's Home shows about now: their errands in this mode, and how many wait for a courier. */
interface Glance {
  mine: Errand[];
  /** Open errands someone else posted, for a courier; null for a requester. */
  waiting: number | null;
}

/** How many of a student's own errands Home lists; My errands has the rest. */
const GLANCE_ROWS = 4;

export function HomeView({ mode, role, onNavigate, ordersApi = null, userId, available = null, held = null }: HomeViewProps) {
  const shortcuts = SHORTCUTS.filter((s) => isViewAvailable(s.view, role, mode));
  const isStudent = role === "STUDENT";
  const [glance, setGlance] = useState<Glance | null>(null);
  const [glanceError, setGlanceError] = useState<string | null>(null);

  usePolling(
    isStudent && ordersApi
      ? async (isActive) => {
          try {
            let next: Glance;
            if (mode === "requesting") {
              next = { mine: (await ordersApi.listRequested("active")).items, waiting: null };
            } else {
              const [assigned, open] = await Promise.all([ordersApi.listAssigned("active"), ordersApi.listOpen()]);
              next = {
                mine: assigned.items,
                waiting: open.items.filter((e) => e.requesterId !== userId).length,
              };
            }
            if (!isActive()) return;
            setGlance(next);
            setGlanceError(null);
          } catch (err) {
            if (isActive()) setGlanceError(orderErrorMessage(err));
            throw err;
          }
        }
      : null,
    10_000,
    [ordersApi, mode, isStudent, userId],
  );

  return (
    <section>
      <div className="hero">
        <h1>Friend on Campus</h1>
        {/* AI-generated (edited by James Teo): an admin's line. */}
        <p className="hero-sub">
          {role === "ADMIN"
            ? "Keep errands moving: settle the ones that have stalled, review the accounts behind them, and see what admins have done."
            : mode === "requesting"
              ? "Someone is already walking past. Ask them to collect it for you, and hold a few credits until it arrives."
              : "Pick up an errand on a walk you were making anyway, and earn the credits held against it."}
        </p>
      </div>

      {isStudent && ordersApi && (
        <div className="home-glance">
          <div className="home-panel">
            <h2 className="section-label">
              {mode === "requesting" ? "Your errands in progress" : "You're carrying"}
            </h2>
            {glanceError && (
              <p className="status error" role="alert">
                {glanceError}
              </p>
            )}
            {!glance && !glanceError && (
              <div role="status" aria-label="Loading your errands">
                <span className="skeleton row" />
                <span className="skeleton row" />
              </div>
            )}
            {glance && glance.mine.length === 0 && (
              <p className="home-empty">
                {mode === "requesting" ? "Nothing in progress." : "You're not carrying anything."}{" "}
                <button
                  type="button"
                  className="link-btn"
                  onClick={() => onNavigate(mode === "requesting" ? "new-errand" : "find-errands")}
                >
                  {mode === "requesting" ? "Post an errand" : "Find errands"}
                </button>
              </p>
            )}
            {glance && glance.mine.length > 0 && (
              <ul className="home-list">
                {glance.mine.slice(0, GLANCE_ROWS).map((e) => (
                  <li key={e.id}>
                    <button type="button" className="home-row" onClick={() => onNavigate("my-errands")}>
                      <span className="home-row-title">{e.itemDescription}</span>
                      <span className={`badge ${stateBadgeClass(e.state)}`}>{STATE_LABEL[e.state] ?? e.state}</span>
                      <span className="home-row-reward">{e.reward} cr</span>
                    </button>
                  </li>
                ))}
              </ul>
            )}
            {glance && glance.mine.length > GLANCE_ROWS && (
              <button type="button" className="link-btn" onClick={() => onNavigate("my-errands")}>
                {glance.mine.length - GLANCE_ROWS} more in My errands
              </button>
            )}
            {glance && glance.waiting !== null && (
              <p className="home-waiting">
                <button type="button" className="link-btn" onClick={() => onNavigate("find-errands")}>
                  {glance.waiting === 0
                    ? "No open errands on offer right now"
                    : `${glance.waiting} open ${glance.waiting === 1 ? "errand" : "errands"} you could take`}
                </button>
              </p>
            )}
          </div>

          <div className="home-panel">
            <h2 className="section-label">Credits</h2>
            <p className="home-balance">
              <strong>{available === null ? "—" : available}</strong> <span>available</span>
            </p>
            {held !== null && held > 0 && <p className="home-held">{held} held on errands in progress</p>}
            <button type="button" className="link-btn" onClick={() => onNavigate("credits")}>
              See your history
            </button>
          </div>
        </div>
      )}

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
