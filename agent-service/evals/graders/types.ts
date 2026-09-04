export interface GradeResult {
  passed: boolean;
  errors: string[];
}

export interface EvalCallRecord {
  name: string;
  params: unknown;
  ts: string;
}

export interface ToolExpectation {
  required?: readonly string[];
  forbidden?: readonly string[];
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
