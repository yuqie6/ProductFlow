import { result, type EvalCallRecord, type GradeResult, type ToolExpectation } from "./types.js";

export function gradeTools(expectation: ToolExpectation | undefined, calls: readonly EvalCallRecord[]): GradeResult {
  if (!expectation) return result([]);
  const names = new Set(calls.map((call) => call.name));
  const errors: string[] = [];
  for (const required of expectation.required ?? []) {
    if (!names.has(required)) errors.push(`required tool was not called: ${required}`);
  }
  for (const forbidden of expectation.forbidden ?? []) {
    if (names.has(forbidden)) errors.push(`forbidden tool was called: ${forbidden}`);
  }
  return result(errors);
}
