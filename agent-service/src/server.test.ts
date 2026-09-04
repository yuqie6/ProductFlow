import { describe, expect, it } from "vitest";
import { ProductFlowClient } from "./productflow.js";
import { DEPLOYED_HARNESS } from "./harness.js";
import { PiRuntimeManager } from "./runtime-manager.js";
import { createHTTPServer } from "./server.js";
import { loadSkillCatalog } from "./skills.js";
import { TurnStore } from "./store.js";

describe("ProductFlow Pi HTTP contract", () => {
  it("reports the explicit runtime and protects internal routes", async () => {
    const store = new TurnStore("/tmp/productflow-pi-server-test");
    await store.init();
    const token = "0123456789abcdef0123456789abcdef";
    const manager = new PiRuntimeManager(
      {
        listenAddress: "127.0.0.1:0",
        dataRoot: "/tmp/productflow-pi-server-test",
        productFlowBaseURL: "http://127.0.0.1:29282",
        internalToken: token,
        requestTimeoutMS: 1000,
        providerRequestTimeoutMS: 5_000,
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
        questionTimeoutMS: 900_000,
      },
      store,
      new ProductFlowClient("http://127.0.0.1:29282", token, 1000),
      await loadSkillCatalog(),
    );
    const server = createHTTPServer(manager, manager.config);
    await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
    const address = server.address();
    if (!address || typeof address === "string") throw new Error("test server did not bind");
    try {
      const health = await fetch(`http://127.0.0.1:${address.port}/healthz`);
      expect(health.status).toBe(200);
      expect(await health.json()).toMatchObject({
        runtime: "productflow-pi",
        harness_hash: DEPLOYED_HARNESS.hash,
        evolution_traces: { enabled: false, pending_records: 0, dropped_records: 0, io_errors: 0, evicted_traces: 0 },
        pi_sdk_version: "0.83.0",
        os_tools: [],
        background_durable_tasks: false,
      });
      const unauthorized = await fetch(`http://127.0.0.1:${address.port}/internal/v1/conversations/test/turns`);
      expect(unauthorized.status).toBe(401);
      const invalid = await fetch(`http://127.0.0.1:${address.port}/internal/v1/conversations/test/turns`, {
        method: "POST",
        headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
        body: JSON.stringify({ input_text: "", asset_ids: [], idempotency_key: "k" }),
      });
      expect(invalid.status).toBe(400);

      const events = await fetch(
        `http://127.0.0.1:${address.port}/internal/v1/conversations/test/turns/turn-1/events?after=5`,
        { headers: { Authorization: `Bearer ${token}` } },
      );
      expect(events.status).toBe(404);
      expect(await events.json()).toMatchObject({ error: { code: "not_found" } });

      const metricsUnauthorized = await fetch(`http://127.0.0.1:${address.port}/metrics`);
      expect(metricsUnauthorized.status).toBe(401);
      const metrics = await fetch(`http://127.0.0.1:${address.port}/metrics`, {
        headers: { Authorization: `Bearer ${token}` },
      });
      expect(metrics.status).toBe(200);
      const body = await metrics.text();
      expect(body).toContain("productflow_agent_service_active_turns");
      expect(body).toContain("productflow_agent_service_background_resumable 0");
      expect(body).not.toContain("conversation_id");
    } finally {
      await manager.close();
      await new Promise<void>((resolve) => server.close(() => resolve()));
    }
  });

  it("does not register /metrics when the internal token is unset", async () => {
    const store = new TurnStore("/tmp/productflow-pi-metrics-unconfigured");
    await store.init();
    const manager = new PiRuntimeManager(
      {
        listenAddress: "127.0.0.1:0",
        dataRoot: "/tmp/productflow-pi-metrics-unconfigured",
        productFlowBaseURL: "http://127.0.0.1:29282",
        internalToken: "",
        requestTimeoutMS: 1000,
        providerRequestTimeoutMS: 5_000,
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
        questionTimeoutMS: 900_000,
      },
      store,
      new ProductFlowClient("http://127.0.0.1:29282", "unused", 1000),
      await loadSkillCatalog(),
    );
    const server = createHTTPServer(manager, manager.config);
    await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
    const address = server.address();
    if (!address || typeof address === "string") throw new Error("test server did not bind");
    try {
      const metrics = await fetch(`http://127.0.0.1:${address.port}/metrics`);
      expect(metrics.status).toBe(404);
    } finally {
      await manager.close();
      await new Promise<void>((resolve) => server.close(() => resolve()));
    }
  });
});
