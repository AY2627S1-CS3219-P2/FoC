// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Session types. 2026-09-20: reshaped to the fields the D1 backlog
//   names (F1.4.1, F1.5) and extended with the pending-registration state the
//   OTP step needs (F1.1.2.3-F1.1.2.7). 2026-09-21: email and contact
//   made optional — user-service supplies neither. 2026-09-21 (later):
//   both are on user-service's profile responses after Zi Yang's 72fe5a5,
//   so the comments saying they have no source were corrected.
// Author review: Nigeltzy - AI was used to create the implementation for the session types and pending registration types. Checked and is valid and works as intended.
// Update was made according to changes in teammate's architecture and framework. Changes made are valid and intended.

/**
 * The signed-in user. Fields follow F1.4.1 (identifier, email, username,
 * contact) and F1.5 (role).
 *
 * `email` and `contact` come from the profile call after login, not from the
 * token, so they are undefined when that call fails. Every view must render
 * without them.
 */
export interface Session {
  /** F1.4.1 "account identifier". F1.4.3 makes it unchangeable. */
  userId: string;
  /** F1.1 registration field. F1.4.2 makes it updatable. */
  username: string;
  /** F1.4.1. F1.4.3 makes it unchangeable. From the profile call, not a claim. */
  email?: string;
  /** F1.4.1 "contact information" — user-service calls it `phone_num`. */
  contact?: string;
  /** F1.5 — the account authorisation role, distinct from the errand role. */
  role: AccountRole;
  /** Rendered in the avatar chip, e.g. "NT". Derived, never sent. */
  initials: string;
}

/**
 * F1.5: the account authorisation role. Requester and courier are not account
 * roles (the backlog's "errand participation role"); ActingMode carries that.
 */
export type AccountRole = "STUDENT" | "ADMIN";

/** Which side of an errand the user is currently acting as. */
export type ActingMode = "requesting" | "delivering";

/**
 * A registration awaiting its OTP.
 *
 * F1.1.2.7 is the reason this type exists: registration completes only after
 * the correct OTP is submitted, so signing up cannot hand back a Session. It
 * hands back this, and the OTP step exchanges it for one.
 */
export interface PendingRegistration {
  /**
   * Opaque handle for the in-progress registration. The UI only passes it
   * back and never reads it.
   */
  handle: string;
  /** Shown back to the user so they know where to look for the code. */
  email: string;
  /** Epoch ms. F1.1.2.5 — a code expires five minutes after it is issued. */
  expiresAt: number;
  /** F1.1.2.6 — how many replacement codes are still allowed. */
  resendsRemaining: number;
  /**
   * Epoch ms, or null when not blocked. F1.1.2.6 — after three replacements
   * inside ten minutes, further requests are blocked for ten minutes.
   */
  blockedUntil: number | null;
  /**
   * Fixture only: the generated code, shown on the OTP step when the fixture
   * client is in use, since there is no mail server in development.
   */
  fixtureCode?: string;
}
