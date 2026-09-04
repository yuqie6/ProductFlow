import { result, type GradeResult } from "./types.js";

export function gradeTerminal(expected: readonly string[] | undefined, actual: string): GradeResult {
  if (!expected || expected.length === 0 || expected.includes(actual)) return result([]);
  return result([`terminal status ${actual} is not one of: ${expected.join(", ")}`]);
}
