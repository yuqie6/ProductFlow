import { result, type EvalCallRecord, type GradeResult, type OperationExpectation } from "./types.js";

export function operationNames(calls: readonly EvalCallRecord[]): string[] {
  return calls.flatMap((call) => {
    if (!call.params || typeof call.params !== "object") return [];
    const operations = (call.params as { operations?: unknown }).operations;
    if (!Array.isArray(operations)) return [];
    return operations.flatMap((operation) => {
      if (!operation || typeof operation !== "object") return [];
      const name = (operation as { op?: unknown }).op;
      return typeof name === "string" ? [name] : [];
    });
  });
}

export function gradeOperations(
  expectation: OperationExpectation | undefined,
  calls: readonly EvalCallRecord[],
): GradeResult {
  if (!expectation) return result([]);
  const names = new Set(operationNames(calls));
  const errors: string[] = [];
  for (const required of expectation.required ?? []) {
    if (!names.has(required)) errors.push(`required Graph operation was not used: ${required}`);
  }
  for (const forbidden of expectation.forbidden ?? []) {
    if (names.has(forbidden)) errors.push(`forbidden Graph operation was used: ${forbidden}`);
  }
  return result(errors);
}
