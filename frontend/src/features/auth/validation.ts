// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-20
// Scope: Client-side validation for the registration fields, transcribing the
//   limits stated in the D1 backlog (F1.1.2.1, F1.1.3.1, F1.1.4.2).
// Author review: Nigeltzy - AI was used to create the implementation for the validation rules and functions. Checked and is valid and works as intended.
// Requirements were decided upon by the team and is reflected in our product backlog.

/**
 * The registration rules, in one place, as pure functions.
 *
 * These are a CONVENIENCE, not a control. Every rule here is also the User
 * Service's to enforce — a client check only saves the user a round trip, and
 * anything that matters is re-checked server-side. Never treat a pass here as
 * authorisation.
 *
 * The limits are transcribed from the D1 milestone backlog and the requirement
 * ID is named beside each, so a change to the backlog has one place to land.
 */

/** F1.1.2.1 — the campus domain registration is restricted to. */
export const NUS_EMAIL_DOMAIN = "@u.nus.edu";

/** F1.1.4.2 — usernames are alphanumeric, up to this length. */
export const USERNAME_MAX = 128;

/** F1.1.3.1 — password length bounds. */
export const PASSWORD_MIN = 8;
export const PASSWORD_MAX = 128;

/** F1.1.2.3 — the OTP is six digits. */
export const OTP_LENGTH = 6;

/**
 * A single password rule and whether the current input satisfies it. The
 * signup form renders these live rather than waiting for a submit, so the user
 * is told what is missing while they can still act on it (F7.1.1).
 */
export interface PasswordCheck {
  label: string;
  met: boolean;
}

/**
 * F1.1.2.1 — the email must end with the campus domain.
 *
 * Returns null when valid, or a message naming what to fix. Comparison is
 * case-insensitive: nothing in the requirement says the user must type it in
 * lower case.
 */
export function validateEmail(email: string): string | null {
  const trimmed = email.trim();
  if (!trimmed) return "Enter your NUS email address.";
  if (!trimmed.toLowerCase().endsWith(NUS_EMAIL_DOMAIN)) {
    return `Use your NUS email address, ending in ${NUS_EMAIL_DOMAIN}.`;
  }
  // Something has to precede the domain — "@u.nus.edu" alone is not an address.
  if (trimmed.length <= NUS_EMAIL_DOMAIN.length) {
    return "Enter the part of your email before the @.";
  }
  return null;
}

/**
 * F1.1.4.2 — alphanumeric only, at most USERNAME_MAX characters.
 *
 * Uniqueness (F1.1.4.1) is deliberately NOT checked here: only the User
 * Service can answer it, and guessing client-side would be wrong as often as
 * it was right.
 */
export function validateUsername(username: string): string | null {
  const trimmed = username.trim();
  if (!trimmed) return "Choose a username.";
  if (!/^[a-zA-Z0-9]+$/.test(trimmed)) {
    return "Usernames can use letters and numbers only.";
  }
  if (trimmed.length > USERNAME_MAX) {
    return `Usernames can be at most ${USERNAME_MAX} characters.`;
  }
  return null;
}

/**
 * F1.1.3.1 — at least one uppercase letter, one lowercase letter, one digit,
 * at least PASSWORD_MIN characters and at most PASSWORD_MAX.
 */
export function passwordChecks(password: string): PasswordCheck[] {
  return [
    {
      label: `At least ${PASSWORD_MIN} characters`,
      met: password.length >= PASSWORD_MIN,
    },
    {
      label: `At most ${PASSWORD_MAX} characters`,
      met: password.length > 0 && password.length <= PASSWORD_MAX,
    },
    { label: "An uppercase letter", met: /[A-Z]/.test(password) },
    { label: "A lowercase letter", met: /[a-z]/.test(password) },
    { label: "A digit", met: /[0-9]/.test(password) },
  ];
}

/** F1.1.3.1 — the same rules as a single pass/fail with a message. */
export function validatePassword(password: string): string | null {
  if (!password) return "Choose a password.";
  const unmet = passwordChecks(password).filter((check) => !check.met);
  if (unmet.length === 0) return null;
  return `Your password still needs: ${unmet
    .map((check) => check.label.toLowerCase())
    .join(", ")}.`;
}

/** F1.1.2.3 — exactly OTP_LENGTH digits. */
export function validateOtp(code: string): string | null {
  const trimmed = code.trim();
  if (!trimmed) return "Enter the code we emailed you.";
  if (!new RegExp(`^[0-9]{${OTP_LENGTH}}$`).test(trimmed)) {
    return `The code is ${OTP_LENGTH} digits.`;
  }
  return null;
}

/**
 * F1.1.1 — every required registration field must be present.
 *
 * Returns the first problem found, field by field in the order the form
 * presents them, so focus lands somewhere sensible.
 */
export function validateRegistration(fields: {
  email: string;
  username: string;
  password: string;
}): string | null {
  return (
    validateEmail(fields.email) ??
    validateUsername(fields.username) ??
    validatePassword(fields.password)
  );
}
