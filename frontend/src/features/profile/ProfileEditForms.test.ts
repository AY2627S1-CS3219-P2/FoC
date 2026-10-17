// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5.5), date: 2026-10-11
// Scope: Tests for the new-password check shared by the change-password and
//   admin reset forms.
// Author review: nigeltzy - Checked that the unit tests were generated and they seem valid and as expected.

import { describe, expect, it } from "vitest";
import { validateNewPassword } from "./ProfileEditForms";

describe("validateNewPassword", () => {
  it("passes a valid password typed twice", () => {
    expect(validateNewPassword("NewPass1", "NewPass1")).toBeNull();
  });

  it("rejects a mismatched confirmation", () => {
    expect(validateNewPassword("NewPass1", "NewPass2")).toContain("don't match");
  });

  it("reports the password rules before the mismatch", () => {
    expect(validateNewPassword("short", "other")).toContain("still needs");
  });

  it("rejects an empty password", () => {
    expect(validateNewPassword("", "")).not.toBeNull();
  });
});
