import { describe, expect, it, vi } from "vitest";
import { ProductFlowClient } from "./productflow.js";
import { PiRuntimeManager } from "./pi-runtime.js";
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

      const event = {
        schema_version: 1 as const,
        run_id: "run-1",
        turn_id: "turn-1",
        sequence: 6,
        created_at: "2026-08-19T00:00:00.000Z",
        kind: "text.delta",
        payload: { delta: "ok" },
      };
      const stream = vi.spyOn(manager, "streamEvents").mockImplementation(async function* (_lookup, _turnID, after) {
        expect(after).toBe(5);
        yield event;
      });
      const events = await fetch(
        `http://127.0.0.1:${address.port}/internal/v1/conversations/test/turns/turn-1/events?after=5`,
        { headers: { Authorization: `Bearer ${token}` } },
      );
      expect(events.status).toBe(200);
      expect(events.headers.get("content-type")).toContain("text/event-stream");
      expect(await events.text()).toBe(
        `id: 6\nevent: text.delta\ndata: ${JSON.stringify(event)}\n\n`,
      );
      expect(stream).toHaveBeenCalledOnce();
    } finally {
      await manager.close();
      await new Promise<void>((resolve) => server.close(() => resolve()));
    }
  });
});
