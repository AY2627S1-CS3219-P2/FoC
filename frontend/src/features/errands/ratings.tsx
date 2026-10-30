// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-29
// Scope: Showing a user's star rating and choosing 1 to 5 stars (D-093).
// Author review: PENDING — <reviewer to complete>

import { useId } from "react";
import type { UserRating } from "./orderTypes";

/** "4.3 (12 ratings)", "5.0 (1 rating)" or "No ratings yet". */
export function ratingText(rating: Pick<UserRating, "count" | "average">): string {
  if (rating.count === 0 || rating.average === undefined) return "No ratings yet";
  const n = rating.count === 1 ? "1 rating" : `${rating.count} ratings`;
  return `${rating.average.toFixed(1)} (${n})`;
}

/**
 * A user's rating as a star and the text beside it. The star is decoration;
 * the text says everything, so a screen reader reads only that.
 */
export function Stars({ rating }: { rating: UserRating | null | undefined }) {
  if (rating === undefined) return null;
  if (rating === null) return <span className="stars muted">Rating unavailable</span>;
  return (
    <span className="stars">
      {rating.count > 0 && (
        <span className="star" aria-hidden="true">
          ★
        </span>
      )}
      <span>{ratingText(rating)}</span>
    </span>
  );
}

interface StarInputProps {
  /** 0 while nothing is chosen. */
  value: number;
  onChange: (stars: number) => void;
  /** What is being rated, for the group's accessible name. */
  label: string;
  disabled?: boolean;
}

/**
 * Five radio buttons drawn as stars, so the arrow keys move between them and
 * each is read out as "3 stars". Stars up to the chosen one are lit.
 */
export function StarInput({ value, onChange, label, disabled = false }: StarInputProps) {
  const name = useId();
  return (
    <div className="star-input" role="radiogroup" aria-label={label}>
      {[1, 2, 3, 4, 5].map((n) => (
        <label key={n} className={n <= value ? "star-option lit" : "star-option"}>
          <input
            type="radio"
            name={name}
            value={n}
            checked={value === n}
            disabled={disabled}
            onChange={() => onChange(n)}
            className="visually-hidden"
          />
          <span aria-hidden="true">★</span>
          <span className="visually-hidden">{n === 1 ? "1 star" : `${n} stars`}</span>
        </label>
      ))}
    </div>
  );
}
