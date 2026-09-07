import type { TranslationKey } from "./i18n";

export function validateDisplayName(value: string): TranslationKey | null {
  const length = [...value.trim()].length;
  return length < 1 || length > 160 ? "account.nameInvalid" : null;
}

export function validateNewPassword(password: string, confirmation: string): TranslationKey | null {
  if ([...password].length < 8 || new TextEncoder().encode(password).length > 72) return "account.passwordInvalid";
  return password !== confirmation ? "account.passwordMismatch" : null;
}
