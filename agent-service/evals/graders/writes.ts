import { isDeepStrictEqual } from "node:util";

import { result, type EvalCallRecord, type GradeResult, type WriteExpectation } from "./types.js";

const PATH_PART = /(?:^|\.)([^.[\]]+)|\[(\d+)\]/gu;

export function valueAtPath(value: unknown, path: string): unknown {
  let current = value;
  let consumed = "";
  for (const match of path.matchAll(PATH_PART)) {
    consumed += match[0];
    const key = match[1] ?? Number(match[2]);
    if (current === null || typeof current !== "object") return undefined;
    current = (current as Record<string | number, unknown>)[key];
  }
  return consumed.replace(/^\./u, "") === path ? current : undefined;
}

export function gradeWrites(
  expectations: readonly WriteExpectation[] | undefined,
  calls: readonly EvalCallRecord[],
): GradeResult {
  if (!expectations) return result([]);
  const errors: string[] = [];
  for (const expectation of expectations) {
    const candidates = calls.filter((call) => call.name === expectation.tool);
    if (candidates.length === 0) {
      errors.push(`expected write tool was not called: ${expectation.tool}`);
      continue;
    }
    const matched = candidates.some((call) => Object.entries(expectation.match).every(([path, expected]) =>
      isDeepStrictEqual(valueAtPath(call.params, path), expected)
    ));
    if (!matched) {
      errors.push(`no ${expectation.tool} call matched ${JSON.stringify(expectation.match)}`);
    }
  }
  return result(errors);
}
