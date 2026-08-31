import { describe, expect, it } from "vitest";
import { formatEvalReport, requireLiveEvalEnv, runLiveSkillEvals } from "./live-runner.js";

const live = process.env.PRODUCTFLOW_RUN_AGENT_EVALS === "1";

describe.skipIf(!live)("ProductFlow live agent evals", () => {
  it("runs the skill fixtures against the configured model and prints call/token report", async () => {
    requireLiveEvalEnv();
    const report = await runLiveSkillEvals();
    process.stdout.write(`${formatEvalReport(report)}\n`);
    expect(report.ok, report.rows.filter((row) => !row.ok).map((row) => `${row.id}: ${row.error}`).join("; ")).toBe(true);
  }, 60 * 60_000);
});
