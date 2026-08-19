import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import type { Scope } from "./contracts.js";
import { PiRuntimeManager } from "./pi-runtime.js";
import { TurnStore } from "./store.js";

const scope: Scope = {
  schema_version: 1,
  scope_type: "product_workflow",
  conversation_id: "11111111-1111-4111-8111-111111111111",
  task_id: null,
  task_goal: null,
  product_id: "22222222-2222-4222-8222-222222222222",
  workflow_draft_id: "33333333-3333-4333-8333-333333333333",
  run_id: "44444444-4444-4444-8444-444444444444",
  system_prompt: "ProductFlow",
  draft_schema: { type: "object" },
  workflow_draft_schema: { type: "object" },
  current_draft_version: 1,
};

const config = {
  listenAddress: "127.0.0.1:0",
  dataRoot: "/tmp/productflow-pi-runtime-test",
  productFlowBaseURL: "http://127.0.0.1:29282",
  internalToken: "0123456789abcdef0123456789abcdef",
  requestTimeoutMS: 1000,
  eventPollIntervalMS: 10,
  heartbeatIntervalMS: 100,
  maxBodyBytes: 1024 * 1024,
  maxIterations: 4,
  modelContextWindow: 128_000,
  autoCompactTokenLimit: 96_000,
  maxConcurrentTurns: 1,
  providerAPIKey: "",
  providerBaseURL: null,
  providerModel: null,
  providerReasoningEffort: null,
  providerReasoningSummary: null,
  providerTextVerbosity: null,
  providerServiceTier: null,
};

describe("PiRuntimeManager turn state", () => {
  it("starts each Turn with a fresh checkpoint sequence", async () => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-runtime-"));
    try {
      const store = new TurnStore(root);
      await store.init();
      const manager = new PiRuntimeManager(
        { ...config, dataRoot: root },
        store,
        {} as ConstructorParameters<typeof PiRuntimeManager>[2],
        {} as ConstructorParameters<typeof PiRuntimeManager>[3],
      );
      const runtime = await (manager as unknown as {
        runtimeFor(input: Scope): Promise<unknown>;
      }).runtimeFor(scope);
      const internal = runtime as {
        checkpointSequence: number;
        resetTurnState(): void;
      };

      internal.checkpointSequence = 6;
      internal.resetTurnState();

      expect(internal.checkpointSequence).toBe(0);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });
});
