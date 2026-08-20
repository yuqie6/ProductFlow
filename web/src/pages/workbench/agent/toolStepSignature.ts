import type { AgentToolStep } from "../../../lib/types";

export function toolStepSignature(steps: readonly AgentToolStep[]): string {
  return JSON.stringify(
    steps.map(({ step_id, kind, status, summary, tool_name: toolName, details }) => [
      step_id,
      kind,
      status,
      summary,
      toolName ?? null,
      details ?? null,
    ]),
  );
}
