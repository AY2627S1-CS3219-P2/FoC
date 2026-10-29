// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-18
// Scope: One errand on a list (My errands, Find errands, All errands): item,
//   supplier and delivery location, a state badge where the list needs one,
//   the reward and a status
//   line, opening the errand's detail, and a note under it such as why an
//   Accept was refused; plus the state badge's colours, and a grey reward
//   on an errand that ended without paying it.
// Author review: James Teo — reviewed and verified.

import type { ReactNode } from "react";
import { STATE_LABEL } from "./format";
import type { Errand, ErrandStatus } from "./orderTypes";

/**
 * The badge colours for a state. Purely visual: the badge always carries the
 * state's name, and nothing here decides what a state permits.
 */
export function stateBadgeClass(state: ErrandStatus): string {
  if (state === "COMPLETED" || state === "DELIVERED") return "badge-available";
  if (state === "CANCELLED" || state === "EXPIRED") return "badge-unavailable";
  return "badge-type";
}

interface ErrandCardProps {
  errand: Errand;
  /** Under the supplier and location: a countdown, what it waits for, or time in state. */
  line: string | null;
  /**
   * Marks a card that waits on the viewer (a delivery to confirm). The line
   * says so in words; this adds weight, not meaning.
   */
  needsYou?: boolean;
  /**
   * False on a list where every errand is in the same state (Find errands
   * lists only OPEN ones), so the badge would say nothing.
   */
  showState?: boolean;
  onOpen: () => void;
  /** Buttons beside the card, such as Accept on Find errands. */
  children?: ReactNode;
  /** A line across the foot of the card, such as why Accept was refused. */
  note?: ReactNode;
  // AI-generated (edited by PENDING): lets Find errands notice when the whole card is in view (D-092).
  /** Given the card's element, such as for noticing when it is in view. */
  cardRef?: (node: HTMLElement | null) => void;
  // AI-generated (edited by PENDING): D-093 names the requester on Find errands.
  /** Who posted it, such as the requester's name and rating. */
  poster?: ReactNode;
}

/**
 * The whole card body is one button that opens the detail, so it is reached
 * and used by keyboard like any other. Contact numbers and IDs are never on a
 * card; they are in the detail only. The one name a card carries is the
 * requester's on Find errands, with their rating (D-093, amending D-075).
 */
export function ErrandCard({
  errand,
  line,
  needsYou = false,
  showState = true,
  onOpen,
  children,
  note,
  cardRef,
  poster,
}: ErrandCardProps) {
  return (
    <article ref={cardRef} className={`errand-card has-open ${needsYou ? "needs-you" : ""}`}>
      <button type="button" className="errand-card-open" onClick={onOpen}>
        <span className="errand-card-body">
          <span className="errand-title">{errand.itemDescription}</span>
          <span className="errand-meta">
            {errand.supplier.name} → {errand.deliveryLocation}
          </span>
          {line && <span className="errand-line">{line}</span>}
          {poster && <span className="errand-poster">{poster}</span>}
        </span>
        <span className="errand-card-side">
          {showState && (
            <span className={`badge ${stateBadgeClass(errand.state)}`}>
              {STATE_LABEL[errand.state] ?? errand.state}
            </span>
          )}
          <RewardText
            reward={errand.reward}
            muted={errand.state === "CANCELLED" || errand.state === "EXPIRED"}
          />
        </span>
      </button>
      {children && <div className="errand-card-actions">{children}</div>}
      {note}
    </article>
  );
}

/**
 * "10 cr" on screen, read out as "10 credits". `muted` greys a reward that
 * was never paid (the errand was cancelled or expired, or the courier
 * withdrew), so it does not look earned or spent.
 */
export function RewardText({ reward, muted = false }: { reward: number; muted?: boolean }) {
  return (
    <span className={muted ? "errand-reward muted" : "errand-reward"}>
      {reward}
      <span aria-hidden="true"> cr</span>
      <span className="visually-hidden"> credits</span>
    </span>
  );
}
