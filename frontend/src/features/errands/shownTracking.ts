// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: Noticing when an errand's whole card is in view on Find errands, so
//   it can be recorded as shown (D-092).
// Author review: PENDING — <reviewer to complete>

import { useCallback, useEffect, useRef } from "react";

/** One errand the courier has seen: which, and where it sat on the list. */
export interface ShownErrand {
  errandId: string;
  /** Its place on the list as shown, 0 for the top. */
  position: number;
}

/**
 * Remembers which errands were already reported as shown during one visit to
 * Find errands, so the five-second refresh does not report the same card
 * again. A new visit starts a new log.
 */
export class ShownLog {
  private readonly seen = new Set<string>();

  /** True the first time an errand is seen this visit, false after. */
  firstSighting(errandId: string): boolean {
    if (this.seen.has(errandId)) return false;
    this.seen.add(errandId);
    return true;
  }
}

/**
 * A card counts as shown only when all of it is inside the viewport. 0.99,
 * not 1, because a card's edge can land on a fraction of a pixel.
 */
const WHOLE_CARD = 0.99;

/**
 * Watches the cards on a list and calls `onShown` the first time each errand's
 * whole card is in view during this visit. `track(errandId, position)` returns
 * the ref callback for that errand's card.
 */
export function useShownTracking(
  onShown: ((shown: ShownErrand) => void) | undefined,
): (errandId: string, position: number) => (node: HTMLElement | null) => void {
  const report = useRef(onShown);
  report.current = onShown;
  const log = useRef(new ShownLog());
  const cards = useRef(new Map<Element, ShownErrand>());
  const observer = useRef<IntersectionObserver | null>(null);

  useEffect(() => {
    if (typeof IntersectionObserver === "undefined") return;
    const io = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (!entry.isIntersecting || entry.intersectionRatio < WHOLE_CARD) continue;
          const card = cards.current.get(entry.target);
          if (card && log.current.firstSighting(card.errandId)) report.current?.(card);
        }
      },
      { threshold: [WHOLE_CARD] },
    );
    observer.current = io;
    for (const node of cards.current.keys()) io.observe(node);
    return () => {
      io.disconnect();
      observer.current = null;
    };
  }, []);

  // One ref callback per errand and position, so a re-render (the countdowns
  // tick every second) hands React the same function and the card is not
  // dropped and watched again.
  const refs = useRef(new Map<string, (node: HTMLElement | null) => void>());

  return useCallback((errandId: string, position: number) => {
    const key = `${position}:${errandId}`;
    let ref = refs.current.get(key);
    if (!ref) {
      ref = (node: HTMLElement | null) => {
        for (const [el, card] of cards.current) {
          if (card.errandId === errandId && el !== node) {
            observer.current?.unobserve(el);
            cards.current.delete(el);
          }
        }
        if (!node) {
          refs.current.delete(key);
          return;
        }
        cards.current.set(node, { errandId, position });
        observer.current?.observe(node);
      };
      refs.current.set(key, ref);
    }
    return ref;
  }, []);
}
