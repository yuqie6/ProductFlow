import { afterEach, describe, expect, it, vi } from "vitest";
import { DEPLOYED_HARNESS } from "../src/harness.js";
import { runLiveEvals } from "./live-runner.js";
import { EvalRunStorage } from "./run-storage.js";
import { runUserSimEvals } from "./user-sim.js";

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllEnvs();
});

describe("eval harness attribution", () => {
  it.each(["l1", "l3", "l5"] as const)("records the deployed artifact before %s execution", async (layer) => {
    vi.stubEnv("PRODUCTFLOW_RUN_AGENT_EVALS", "1");
    vi.stubEnv("AGENT_PROVIDER_API_KEY", "unused-metadata-test");
    vi.stubEnv("PRODUCTFLOW_AGENT_EVAL_FILTER", "");
    const stop = new Error("metadata boundary reached");
    const init = vi.spyOn(EvalRunStorage.prototype, "init").mockRejectedValue(stop);
    const run = layer === "l3" ? runUserSimEvals() : runLiveEvals({ layer });
    await expect(run).rejects.toBe(stop);
    expect(init).toHaveBeenCalledWith(expect.objectContaining({
      harness_hash: DEPLOYED_HARNESS.hash,
      skill_catalog_hash: expect.any(String),
      task_set_hash: expect.any(String),
      layers: [layer],
    }));
    expect(DEPLOYED_HARNESS.hash).toMatch(/^[a-f0-9]{64}$/);
  });
});
