import { describe, expect, it } from "vitest";

import type { AgentToolStep } from "../../lib/types";
import { toolStepSignature } from "./toolStepSignature";

const steps: AgentToolStep[] = [
  {
    step_id: "step-1",
    kind: "inspect_context",
    status: "running",
    summary: "Read context",
  },
  {
    step_id: "step-2",
    kind: "inspect_image",
    status: "succeeded",
    summary: "Inspect image",
  },
];

describe("toolStepSignature", () => {
  it("is stable for equivalent ordered tool-step lists", () => {
    expect(toolStepSignature(steps.map((step) => ({ ...step })))).toBe(toolStepSignature(steps));
  });

  it.each(["step_id", "kind", "status", "summary"] as const)(
    "changes when ordered step %s changes",
    (field) => {
      const changed = steps.map((step) => ({ ...step }));
      changed[0] = {
        ...changed[0],
        [field]: field === "kind"
          ? "read_history"
          : field === "status"
            ? "failed"
            : `${changed[0][field]}-changed`,
      };

      expect(toolStepSignature(changed)).not.toBe(toolStepSignature(steps));
    },
  );

  it("changes when step order changes", () => {
    expect(toolStepSignature([steps[1], steps[0]])).not.toBe(toolStepSignature(steps));
  });
});
