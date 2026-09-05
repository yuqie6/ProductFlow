import { result, type EvalCallRecord, type GradeResult, type ToolExpectation } from "./types.js";
import { isBusinessWrite, matchesWrite } from "./writes.js";

export function gradeTools(expectation: ToolExpectation | undefined, calls: readonly EvalCallRecord[]): GradeResult {
  if (!expectation) return result([]);
  const names = new Set(calls.map((call) => call.name));
  const completed = new Set(calls.filter((call) => call.outcome === "succeeded").map((call) => call.name));
  const errors: string[] = [];
  for (const required of expectation.required ?? []) {
    if (!completed.has(required)) errors.push(`required tool was not called: ${required}`);
  }
  for (const forbidden of expectation.forbidden ?? []) {
    if (names.has(forbidden)) errors.push(`forbidden tool was called: ${forbidden}`);
  }
  const firstWrite = calls.findIndex((call) => isBusinessWrite(call.name));
  const preceding = firstWrite < 0 ? calls : calls.slice(0, firstWrite);
  for (const read of expectation.reads ?? []) {
    if (!preceding.some((call) => call.name === read.tool && call.outcome === "succeeded" && matchesWrite(read, call.params))) {
      errors.push(`required read was not observed before writing: ${read.tool} ${JSON.stringify(read.match)}`);
    }
  }
  return result(errors);
}
