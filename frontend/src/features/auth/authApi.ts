// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: MOCK client standing in for user-service.
// Author review: PENDING — <reviewer to complete>

import { mockDelay } from "../../lib/mock";
import type { Session } from "./types";

/**
 * ███ MOCK — `user-service` does not exist (M2 is unimplemented). ███
 *
 * This authenticates nothing. It accepts any NUS-looking email, ignores the
 * password entirely, and hands back a fixture session so the shell has a user
 * to render. No credential is transmitted, stored or checked anywhere.
 *
 * Replace this whole file with the generated client once user-service ships;
 * the real identity payload and the real auth mechanism are its owner's
 * decisions (root AGENTS.md §1). Nothing here may be treated as their spec.
 */

export class AuthError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "AuthError";
  }
}

function initialsOf(name: string): string {
  return name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((part) => part[0]?.toUpperCase() ?? "")
    .join("");
}

function nameFromEmail(email: string): string {
  const local = email.split("@")[0] ?? "student";
  return local
    .split(/[._-]+/)
    .filter(Boolean)
    .map((part) => part[0]!.toUpperCase() + part.slice(1))
    .join(" ");
}

export async function logIn(email: string): Promise<Session> {
  if (!email.trim()) throw new AuthError("Enter your NUS email.");
  const name = nameFromEmail(email);
  return mockDelay({
    userId: "mock-user-1",
    name,
    initials: initialsOf(name) || "NU",
    email,
  });
}

export async function signUp(email: string, name: string): Promise<Session> {
  if (!email.trim()) throw new AuthError("Enter your NUS email.");
  if (!name.trim()) throw new AuthError("Enter your name.");
  return mockDelay({
    userId: "mock-user-1",
    name,
    initials: initialsOf(name) || "NU",
    email,
  });
}
