import { describe, expect, it } from "vitest";
import { validateDisplayName, validateNewPassword } from "./accountValidation";

describe("account form constraints", () => {
  it("normalizes display-name whitespace and counts Unicode characters", () => {
    expect(validateDisplayName("   ")).toBe("account.nameInvalid");
    expect(validateDisplayName(" x ")).toBeNull();
    expect(validateDisplayName("😀".repeat(160))).toBeNull();
    expect(validateDisplayName("😀".repeat(161))).toBe("account.nameInvalid");
  });
  it("matches the password character minimum and bcrypt byte maximum", () => {
    expect(validateNewPassword("😀".repeat(7), "😀".repeat(7))).toBe("account.passwordInvalid");
    expect(validateNewPassword("密".repeat(24), "密".repeat(24))).toBeNull();
    expect(validateNewPassword("密".repeat(25), "密".repeat(25))).toBe("account.passwordInvalid");
    expect(validateNewPassword("a".repeat(72), "a".repeat(72))).toBeNull();
    expect(validateNewPassword("a".repeat(73), "a".repeat(73))).toBe("account.passwordInvalid");
    expect(validateNewPassword("password1", "password2")).toBe("account.passwordMismatch");
  });
});
