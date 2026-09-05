import { describe, expect, it } from "vitest";

import { resolveLiveEvalConcurrency } from "./live-concurrency.js";
import { mergeToolCalls } from "./live-runner.js";
import type { EvalCallRecord } from "./schema.js";
import { toolKind } from "../src/tool-manifest.js";

const live = process.env.PRODUCTFLOW_RUN_AGENT_EVALS === "1";

describe("live eval concurrency", () => {
  it("does not duplicate a successful write after a local validation failure", () => {
    const write: EvalCallRecord = { name: "apply_graph_change_set_v1", params: { operations: [{ op: "rename_node", node_ref: "n", title: "ok" }] }, ts: "t", outcome: "succeeded" };
    const merged = mergeToolCalls({ updated_at: "t", tool_steps: [
      { step_id: "bad", kind: toolKind(write.name), tool_name: write.name, status: "failed", summary: "invalid schema" },
      { step_id: "good", kind: toolKind(write.name), tool_name: write.name, status: "succeeded", summary: "applied" },
    ] }, [write]);
    expect(merged.filter((call) => call.outcome === "succeeded")).toEqual([write]);
    expect(merged.filter((call) => call.outcome === "failed")).toHaveLength(1);
  });
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
