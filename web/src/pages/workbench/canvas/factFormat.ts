import type { JsonValue } from "../../../lib/types";

export function humanizeFactKey(key: string): string {
  return key
    .replace(/[._-]+/g, " ")
    .replace(/\s+/g, " ")
    .trim()
    .replace(/(^|\s)\S/g, (character) => character.toUpperCase());
}

export function formatFactValue(
  value: JsonValue,
  noValue: string,
  booleanLabels: { true: string; false: string } = { true: "Yes", false: "No" },
): string {
  if (value === null || value === "") return noValue;
  if (typeof value === "string" || typeof value === "number") return String(value);
  if (typeof value === "boolean") return value ? booleanLabels.true : booleanLabels.false;
  if (Array.isArray(value)) {
    return value.length
      ? value.map((item) => formatFactValue(item, noValue, booleanLabels)).join(" · ")
      : noValue;
  }
  const entries = Object.entries(value);
  if (!entries.length) return noValue;
  return entries
    .map(([key, item]) => `${humanizeFactKey(key)}: ${formatFactValue(item, noValue, booleanLabels)}`)
    .join("; ");
}
