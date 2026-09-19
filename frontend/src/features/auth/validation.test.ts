// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-20
// Scope: Unit tests for the registration validation rules.
// Author review: PENDING — <reviewer to complete>

import { describe, expect, it } from "vitest";
import {
  validateEmail,
  validateOtp,
  validatePassword,
  validateRegistration,
  validateUsername,
} from "./validation";

// Table-driven, error paths first, per root AGENTS.md §7.

describe("validateEmail (F1.1.2.1)", () => {
  const rejected: [string, string][] = [
    ["", "empty"],
    ["   ", "whitespace only"],
    ["nigel@gmail.com", "wrong domain"],
    ["nigel@nus.edu", "close but not the campus domain"],
    ["nigel@u.nus.edu.evil.com", "domain as a prefix, not a suffix"],
    ["@u.nus.edu", "nothing before the @"],
  ];
  it.each(rejected)("rejects %j (%s)", (input) => {
    expect(validateEmail(input)).not.toBeNull();
  });

  it.each([
    ["nigel@u.nus.edu"],
    ["e1234567@u.nus.edu"],
    ["NIGEL@U.NUS.EDU"], // case is not the user's problem
    ["  nigel@u.nus.edu  "],
  ])("accepts %j", (input) => {
    expect(validateEmail(input)).toBeNull();
  });
});

describe("validateUsername (F1.1.4.2)", () => {
  it.each([
    ["", "empty"],
    ["nigel tzy", "space"],
    ["nigel_tzy", "underscore"],
    ["nigel-tzy", "hyphen"],
    ["nigel.tzy", "dot"],
    ["ni@gel", "symbol"],
  ])("rejects %j (%s)", (input) => {
    expect(validateUsername(input)).not.toBeNull();
  });

  it("rejects a username longer than 128 characters", () => {
    expect(validateUsername("a".repeat(129))).not.toBeNull();
  });

  it("accepts exactly 128 characters", () => {
    expect(validateUsername("a".repeat(128))).toBeNull();
  });

  it.each([["nigeltzy"], ["Nigel123"], ["A1"]])("accepts %j", (input) => {
    expect(validateUsername(input)).toBeNull();
  });
});

describe("validatePassword (F1.1.3.1)", () => {
  it.each([
    ["", "empty"],
    ["Ab1", "too short"],
    ["abcdefg1", "no uppercase"],
    ["ABCDEFG1", "no lowercase"],
    ["Abcdefgh", "no digit"],
  ])("rejects %j (%s)", (input) => {
    expect(validatePassword(input)).not.toBeNull();
  });

  it("rejects a password longer than 128 characters", () => {
    expect(validatePassword("Aa1" + "b".repeat(126))).not.toBeNull();
  });

  it("accepts the shortest password meeting every rule", () => {
    expect(validatePassword("Abcdefg1")).toBeNull();
  });

  it("accepts exactly 128 characters", () => {
    expect(validatePassword("Aa1" + "b".repeat(125))).toBeNull();
  });

  it("names every unmet rule, not just the first", () => {
    const message = validatePassword("abc")!;
    expect(message).toContain("8 characters");
    expect(message).toContain("uppercase");
    expect(message).toContain("digit");
  });
});

describe("validateOtp (F1.1.2.3)", () => {
  it.each([["", "empty"], ["12345", "five digits"], ["1234567", "seven digits"], ["12345a", "not all digits"]])(
    "rejects %j (%s)",
    (input) => {
      expect(validateOtp(input)).not.toBeNull();
    },
  );

  it("accepts six digits", () => {
    expect(validateOtp("123456")).toBeNull();
    expect(validateOtp("000000")).toBeNull();
  });
});

describe("validateRegistration (F1.1.1)", () => {
  const good = {
    email: "nigel@u.nus.edu",
    username: "nigeltzy",
    password: "Abcdefg1",
  };

  it("passes when every field is valid", () => {
    expect(validateRegistration(good)).toBeNull();
  });

  it.each(["email", "username", "password"] as const)(
    "rejects when %s is missing",
    (field) => {
      expect(validateRegistration({ ...good, [field]: "" })).not.toBeNull();
    },
  );

  it("reports the email problem first, so focus lands on the first field", () => {
    const message = validateRegistration({
      email: "nigel@gmail.com",
      username: "bad user",
      password: "x",
    });
    expect(message).toContain("u.nus.edu");
  });
});
