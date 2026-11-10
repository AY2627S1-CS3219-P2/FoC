// AI Assistance Disclosure:
// Tool: Claude Code (model: Sonnet 5.5), date: 2026-11-09
// Scope: Which row an arrow, Home or End key moves to in a list, for the
//   workspace layout's keyboard navigation.
// Author review: PENDING — <reviewer to complete>

/**
 * The index to move focus to for `key` from `current` in a list of `length`
 * rows, stopping at the ends; null for a key that does not move. With nothing
 * focused yet (`current` below 0) the first row is the target for a move down
 * and the last for a move up.
 */
export function nextListIndex(current: number, key: string, length: number): number | null {
  if (length <= 0) return null;
  const last = length - 1;
  switch (key) {
    case "ArrowDown":
      return current < 0 ? 0 : Math.min(current + 1, last);
    case "ArrowUp":
      return current < 0 ? last : Math.max(current - 1, 0);
    case "Home":
      return 0;
    case "End":
      return last;
    default:
      return null;
  }
}
