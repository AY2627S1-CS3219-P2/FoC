// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-20
// Scope: In-browser stand-in for the OTP half of registration, so the flow can
//   be built and exercised before user-service exists. Enforces the timing and
//   rate limits from F1.1.2.4-F1.1.2.7 rather than rubber-stamping them.
// Author review: Nigeltzy - AI was used to create the implementation for the OTP fixture and its functions., though OTP is not yet set up. This is just boilerplate.
// Values used are default values, if the team wants to change the values, it will be reflected in decision log.

import { AuthError } from "./authApi";
import { OTP_LENGTH } from "./validation";
import type { PendingRegistration } from "./types";

/**
 * A FIXTURE. Nothing here is security, and none of it survives a page reload.
 *
 * It exists because the OTP flow is five High-priority requirements
 * (F1.1.2.3-F1.1.2.7) that cannot otherwise be built or demonstrated: there is
 * no user-service to issue a code and no mail server to deliver one. The rules
 * are implemented faithfully so the screens meet real behaviour — a code that
 * actually expires, a resend that actually invalidates its predecessor, and a
 * limit that actually locks — rather than a stub that always says yes.
 *
 * The real enforcement is user-service's. Every rule below is re-checked
 * there, and this whole module is deleted when the gateway client takes over.
 */

/** F1.1.2.5 — a code expires five minutes after it is issued. */
const CODE_TTL_MS = 5 * 60 * 1000;

/** F1.1.2.6 — at most three replacements inside a ten-minute window... */
const MAX_RESENDS = 3;
const RESEND_WINDOW_MS = 10 * 60 * 1000;

/** ...after which further requests are blocked for ten minutes. */
const BLOCK_MS = 10 * 60 * 1000;

interface Record_ {
  handle: string;
  email: string;
  username: string;
  /** The code currently valid. Replaced, never appended to (F1.1.2.5). */
  code: string;
  expiresAt: number;
  /** Epoch ms of each resend, used to apply the sliding window. */
  resendTimes: number[];
  blockedUntil: number | null;
}

const pending = new Map<string, Record_>();

function sixDigits(): string {
  // Math.random is fine and appropriate here: this is a development fixture,
  // not a credential. user-service generates the real one.
  return String(Math.floor(Math.random() * 10 ** OTP_LENGTH)).padStart(
    OTP_LENGTH,
    "0",
  );
}

function view(record: Record_): PendingRegistration {
  const recent = record.resendTimes.filter(
    (at) => Date.now() - at < RESEND_WINDOW_MS,
  );
  return {
    handle: record.handle,
    email: record.email,
    expiresAt: record.expiresAt,
    resendsRemaining: Math.max(0, MAX_RESENDS - recent.length),
    blockedUntil: record.blockedUntil,
    fixtureCode: record.code,
  };
}

/** F1.1.2.3 — issue the first code and hold the registration open. */
export function startRegistration(
  email: string,
  username: string,
): PendingRegistration {
  const handle = `fixture-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
  const record: Record_ = {
    handle,
    email,
    username,
    code: sixDigits(),
    expiresAt: Date.now() + CODE_TTL_MS,
    resendTimes: [],
    blockedUntil: null,
  };
  pending.set(handle, record);
  return view(record);
}

/**
 * F1.1.2.4 — issue a replacement code.
 *
 * F1.1.2.5: the previous code is overwritten, so it stops working the moment
 * this returns. F1.1.2.6: three inside ten minutes, then a ten-minute block.
 */
export function resend(handle: string): PendingRegistration {
  const record = pending.get(handle);
  if (!record) throw new AuthError("Start again — that registration expired.");

  const now = Date.now();
  if (record.blockedUntil && now < record.blockedUntil) {
    throw new AuthError(blockedMessage(record.blockedUntil));
  }
  if (record.blockedUntil && now >= record.blockedUntil) {
    // Block served: clear it and start the window fresh.
    record.blockedUntil = null;
    record.resendTimes = [];
  }

  const recent = record.resendTimes.filter((at) => now - at < RESEND_WINDOW_MS);
  if (recent.length >= MAX_RESENDS) {
    record.blockedUntil = now + BLOCK_MS;
    record.resendTimes = recent;
    throw new AuthError(blockedMessage(record.blockedUntil));
  }

  record.code = sixDigits();
  record.expiresAt = now + CODE_TTL_MS;
  record.resendTimes = [...recent, now];
  return view(record);
}

/**
 * F1.1.2.7 — registration completes only on the correct, unexpired code.
 *
 * Returns the username the registration was started with, so the caller can
 * build the session. Consumes the record: a code works once.
 */
export function verify(handle: string, code: string): { username: string; email: string } {
  const record = pending.get(handle);
  if (!record) throw new AuthError("Start again — that registration expired.");

  if (Date.now() > record.expiresAt) {
    throw new AuthError("That code has expired. Ask for a new one.");
  }
  if (code.trim() !== record.code) {
    // Deliberately not saying whether the code was wrong or merely stale.
    throw new AuthError("That code is not right. Check it and try again.");
  }

  pending.delete(record.handle);
  return { username: record.username, email: record.email };
}

/** Re-read the current state, e.g. to refresh a countdown after a tick. */
export function peek(handle: string): PendingRegistration | null {
  const record = pending.get(handle);
  return record ? view(record) : null;
}

function blockedMessage(until: number): string {
  const minutes = Math.max(1, Math.ceil((until - Date.now()) / 60000));
  return `Too many code requests. Try again in about ${minutes} minute${
    minutes === 1 ? "" : "s"
  }.`;
}
