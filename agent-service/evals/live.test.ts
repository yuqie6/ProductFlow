import { describe, expect, it } from "vitest";

import { resolveLiveEvalConcurrency } from "./live-concurrency.js";

const live = process.env.PRODUCTFLOW_RUN_AGENT_EVALS === "1";

describe("live eval concurrency", () => {
  it("caps requested concurrency at production maxConcurrentTurns", () => {
    expect(resolveLiveEvalConcurrency(99, {})).toEqual({
      concurrency: 3,
      productionMaxConcurrentTurns: 3,
    });
    expect(resolveLiveEvalConcurrency(99, { AGENT_MAX_CONCURRENT_TURNS: "2" })).toEqual({
      concurrency: 2,
      productionMaxConcurrentTurns: 2,
    });
    expect(resolveLiveEvalConcurrency(undefined, { AGENT_MAX_CONCURRENT_TURNS: "1" })).toEqual({
      concurrency: 1,
      productionMaxConcurrentTurns: 1,
    });
    expect(resolveLiveEvalConcurrency(2, { AGENT_MAX_CONCURRENT_TURNS: "8" })).toEqual({
      concurrency: 2,
      productionMaxConcurrentTurns: 8,
    });
  });
});

describe.skipIf(!live)("ProductFlow live agent evals", () => {
  it("runs the skill fixtures against the configured model and prints call/token report", async () => {
    const { formatEvalReport, requireLiveEvalEnv, runLiveEvals } = await import("./live-runner.js");
    requireLiveEvalEnv();
    const result = await runLiveEvals();
    process.stdout.write(`${formatEvalReport(result.report)}\n`);
    expect(
      result.report.ok,
      result.report.rows.filter((row) => !row.ok).map((row) => `${row.id}: ${row.error}`).join("; "),
    ).toBe(true);
  }, 60 * 60_000);
});
