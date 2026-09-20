// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: Session types. 2026-09-20: reshaped to the fields the D1 backlog
//   names (F1.4.1, F1.5) and extended with the pending-registration state the
//   OTP step needs (F1.1.2.3-F1.1.2.7). 2026-09-21: email and contact
//   made optional — user-service supplies neither.
// Author review: PENDING — <reviewer to complete>

/**
 * The signed-in user, as far as the UI can actually know them.
 *
 * The FIELDS are taken from the backlog rather than invented: F1.4.1 says a
 * user can view their account identifier, registered email, username and
 * contact information, and F1.5 makes the role STUDENT or ADMIN.
 *
 * TWO OF THEM HAVE NO SOURCE, which is why they are optional. user-service is
 * live now, and its `UserResponse` carries `uid`, `username`, `account_role`,
 * `account_status` and `date_created` — no email. Contact is absent from that
 * service altogether: it is not on `RegisterRequest`, not on `UserResponse`,
 * not in the User DB. So against the real stack these two arrive undefined and
 * every view must render without them. The fixtures still populate both, which
 * is exactly the difference the mock badge is there to advertise.
 *
 * This is a gap in user-service's contract against F1.4.1, not something to
 * paper over here — adding a field is an interface change owned by that
 * service (root AGENTS.md §3, §8). Flagged for Zi Yang.
 */
export interface Session {
  /** F1.4.1 "account identifier". F1.4.3 makes it unchangeable. */
  userId: string;
  /** F1.1 registration field. F1.4.2 makes it updatable. */
  username: string;
  /** F1.4.1. F1.4.3 makes it unchangeable. Absent from user-service today. */
  email?: string;
  /** F1.4.1 "contact information". No field for it exists in user-service. */
  contact?: string;
  /** F1.5 — the account authorisation role, distinct from the errand role. */
  role: AccountRole;
  /** Rendered in the avatar chip, e.g. "NT". Derived, never sent. */
  initials: string;
}

/**
 * F1.5 — the account authorisation role.
 *
 * Deliberately NOT the requester/courier distinction: the backlog glossary
 * calls that the "errand participation role" and says it is not a permanent
 * account role. ActingMode below carries that instead.
 */
export type AccountRole = "STUDENT" | "ADMIN";

/**
 * Which side of an errand the user is currently acting as. Two named modes
 * rather than a boolean, per the control-coupling rule in root AGENTS.md §5.
 */
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
   * Opaque handle for the in-progress registration. The client never
   * constructs or interprets it — user-service decides what it is.
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
   * FIXTURE ONLY, and never present against a real service. There is no mail
   * server in development, so the fixture surfaces the code it generated
   * rather than leaving the flow impossible to exercise. The UI renders it
   * only inside the mock badge.
   */
  fixtureCode?: string;
}
