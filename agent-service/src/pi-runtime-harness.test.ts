import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it, vi } from "vitest";
import { createAgentSession, type AgentSession } from "@earendil-works/pi-coding-agent";
import type { Config } from "./config.js";
import type { RuntimeContext } from "./contracts.js";
import { loadHarness } from "./harness.js";
import { PiSessionAdapter, type PiSessionHost } from "./pi-runtime.js";
import { loadSkillCatalog } from "./skills.js";
import { TurnStore } from "./store.js";

vi.mock("@earendil-works/pi-coding-agent", async (importOriginal) => {
  const original = await importOriginal<typeof import("@earendil-works/pi-coding-agent")>();
  return { ...original, createAgentSession: vi.fn() };
});

describe("Pi production harness assembly", () => {
  it.each([false, true])("passes the frozen harness and reports resolved SDK config (overrides=%s)", async (overrides) => {
    const root = await mkdtemp(join(tmpdir(), "productflow-pi-harness-"));
    try {
      const store = new TurnStore(root);
      await store.init();
      const skills = await loadSkillCatalog();
      const subscribe = vi.fn();
      vi.mocked(createAgentSession).mockResolvedValue({
        session: { subscribe } as unknown as AgentSession,
      } as Awaited<ReturnType<typeof createAgentSession>>);
      const host = {
        config: {
          listenAddress: "127.0.0.1:0", dataRoot: root, productFlowBaseURL: "http://127.0.0.1:29282",
          internalToken: "0123456789abcdef0123456789abcdef", requestTimeoutMS: 1000,
          providerRequestTimeoutMS: 5000, maxBodyBytes: 1024 * 1024, maxIterations: 4,
          modelContextWindow: 128_000, autoCompactTokenLimit: 96_000, maxConcurrentTurns: 1,
          providerAPIKey: "test-key", providerBaseURL: null, providerModel: null,
          providerReasoningEffort: null, providerReasoningSummary: null, providerTextVerbosity: null,
          providerServiceTier: null, questionTimeoutMS: 900_000,
        } satisfies Config,
        store,
        skills,
        scope: {
          schema_version: 1,
          scope_type: "product_workflow",
          conversation_id: "11111111-1111-4111-8111-111111111111",
          merchant_id: "merchant-test",
          product_id: "22222222-2222-4222-8222-222222222222",
          run_id: "44444444-4444-4444-8444-444444444444",
          system_prompt: "ProductFlow product scope",
          task_id: null,
          task_goal: "Preserve the merchant's confirmed image choices",
          draft_schema: {},
          current_draft_version: 0,
          has_live_graph: true,
        },
        client: {
          providerConfig: async () => ({
            schema_version: 1, provider_kind: "openai", model: "fixture-model", api_key: "test-key",
            base_url: null, reasoning_effort: null, reasoning_summary: null, text_verbosity: null,
            service_tier: null, background_resumable: false,
          }),
        },
        signal: new AbortController().signal,
        executionProjectionID: null,
        currentModelRequestIDValue: undefined,
        setCurrentPageType: vi.fn(),
        setJournalToolStep: vi.fn().mockResolvedValue(undefined),
      } as unknown as PiSessionHost;
      if (overrides) {
        host.config.providerModel = "override-model";
        host.config.providerBaseURL = "https://fixture.invalid/v1?token=endpoint-secret";
        host.config.providerReasoningEffort = "HIGH";
        host.config.providerReasoningSummary = "none";
        host.config.providerTextVerbosity = "low";
        host.config.providerServiceTier = "priority";
      }
      const adapter = new PiSessionAdapter(host);
      expect(() => adapter.requestConfiguration).toThrow("has not been resolved");
      const onEvent = vi.fn();
      await adapter.createSession("turn-harness", {} as RuntimeContext, {
        input_text: "Inspect the workflow", asset_ids: [], idempotency_key: "harness-assembly", page_context: null,
      }, onEvent);
      const options = vi.mocked(createAgentSession).mock.calls.at(-1)?.[0];
      const prompt = options?.resourceLoader?.getSystemPrompt();
      expect(prompt).toContain(loadHarness().systemPrompt);
      expect(prompt).toContain("Preserve the merchant's confirmed image choices");
      expect(prompt).toContain(skills.promptForScope("product_workflow"));
      expect(prompt).toContain("<productflow_context");
      expect(options?.noTools).toBe("all");
      expect(subscribe).toHaveBeenCalledWith(onEvent);
      expect(adapter.requestConfiguration).toMatchObject({
        schema_version: 1, provider: "openai", model: overrides ? "override-model" : "fixture-model", api: "openai-responses",
        thinking_level: overrides ? "high" : "medium", reasoning_summary: overrides ? "none" : null,
        text_verbosity: overrides ? "low" : null, service_tier: overrides ? "priority" : null,
      });
      expect(adapter.requestConfiguration.base_url_hash).toMatch(/^[a-f0-9]{64}$/u);
      expect(JSON.stringify(adapter.requestConfiguration)).not.toContain("test-key");
      expect(JSON.stringify(adapter.requestConfiguration)).not.toContain("endpoint-secret");
      host.config.providerModel = "later-config-change";
      expect(adapter.requestConfiguration.model).toBe(overrides ? "override-model" : "fixture-model");
    } finally {
      await rm(root, { recursive: true, force: true });
      vi.clearAllMocks();
    }
  });
});
