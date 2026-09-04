import { result, type BudgetExpectation, type GradeResult } from "./types.js";

export interface TrialUsage {
  tool_calls: number;
  tokens: number | null;
  duration_ms: number;
}

export function gradeBudget(expectation: BudgetExpectation | undefined, usage: TrialUsage): GradeResult {
  if (!expectation) return result([]);
  const errors: string[] = [];
  if (expectation.max_tool_calls !== undefined && usage.tool_calls > expectation.max_tool_calls) {
    errors.push(`tool calls ${usage.tool_calls} exceed budget ${expectation.max_tool_calls}`);
  }
  if (expectation.max_tokens !== undefined) {
    if (usage.tokens === null) errors.push("token usage is unavailable");
    else if (usage.tokens > expectation.max_tokens) errors.push(`tokens ${usage.tokens} exceed budget ${expectation.max_tokens}`);
  }
  if (expectation.max_duration_ms !== undefined && usage.duration_ms > expectation.max_duration_ms) {
    errors.push(`duration ${usage.duration_ms}ms exceeds budget ${expectation.max_duration_ms}ms`);
  }
  return result(errors);
}
