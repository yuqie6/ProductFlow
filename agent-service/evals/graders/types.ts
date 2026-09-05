export interface GradeResult {
  passed: boolean;
  errors: string[];
}

export type { EvalCallRecord } from "../schema.js";

export interface ToolExpectation {
  required?: readonly string[];
  forbidden?: readonly string[];
  reads?: readonly WriteExpectation[];
}

export interface OperationExpectation {
  required?: readonly string[];
  forbidden?: readonly string[];
}

export interface WriteExpectation {
  tool: string;
  match: Readonly<Record<string, unknown>>;
}

export interface BudgetExpectation {
  max_tool_calls?: number;
  max_tokens?: number;
  max_duration_ms?: number;
}

export function result(errors: string[]): GradeResult {
  return { passed: errors.length === 0, errors };
}
