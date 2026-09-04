import { describe, expect, it } from "vitest";

import { cohenKappa, parseJudgeJSON, parseRubric } from "./graders/judge.js";
import { readFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const rubricsRoot = join(dirname(fileURLToPath(import.meta.url)), "rubrics");

describe("L4 judge calibration helpers", () => {
  it("parses 3 to 5 rubric dimensions per skill", async () => {
    for (const skill of ["product-intake", "graph-editing", "workflow-run-request", "media-library-organization", "run-diagnosis"]) {
      const markdown = await readFile(join(rubricsRoot, `${skill}.md`), "utf8");
      const dimensions = parseRubric(skill, markdown);
      expect(dimensions.length).toBeGreaterThanOrEqual(3);
      expect(dimensions.length).toBeLessThanOrEqual(5);
    }
  });

  it("parses judge JSON and computes Cohen kappa", () => {
    expect(parseJudgeJSON('note {"score":0.8,"reason":"一致","unknown":false}')).toMatchObject({ score: 0.8, unknown: false });
    expect(cohenKappa([1, 1, 0, 0], [1, 1, 0, 0])).toBe(1);
    expect(cohenKappa([1, 1, 0, 0], [0, 0, 1, 1])).toBe(-1);
    expect(cohenKappa([1, 1, 1, 0], [1, 1, 0, 0])).toBeCloseTo(0.5);
  });

  it("maps task ids onto skill rubrics by longest prefix", async () => {
    const { skillFromTaskID, loadRubrics } = await import("./graders/judge.js");
    const rubrics = await loadRubrics();
    expect(skillFromTaskID("workflow-run-request-retry-failed-run", rubrics)).toBe("workflow-run-request");
    expect(skillFromTaskID("run-diagnosis-inspect-failed-node", rubrics)).toBe("run-diagnosis");
  });
});
