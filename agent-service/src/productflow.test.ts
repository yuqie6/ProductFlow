import { createServer } from "node:http";
import { describe, expect, it } from "vitest";
import { ProductFlowClient } from "./productflow.js";

describe("ProductFlowClient", () => {
  it("sends only the strict workflow request contract after preparation", async () => {
    const requests: Array<{ url: string; body: Record<string, unknown> }> = [];
    const server = createServer(async (request, response) => {
      const chunks: Buffer[] = [];
      for await (const chunk of request) chunks.push(Buffer.from(chunk));
      requests.push({ url: request.url ?? "", body: JSON.parse(Buffer.concat(chunks).toString("utf8")) as Record<string, unknown> });
      response.writeHead(200, { "Content-Type": "application/json" });
      response.end(JSON.stringify({ accepted: true }));
    });
    await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
    const address = server.address();
    if (!address || typeof address === "string") throw new Error("test server did not bind");

    try {
      const client = new ProductFlowClient(`http://127.0.0.1:${address.port}`, "0123456789abcdef0123456789abcdef", 1000);
      const prepared = {
        product_id: "product-1",
        workflow_id: "workflow-1",
        workflow_title: "Workflow",
        workflow_revision: 7,
        runnable_node_count: 2,
        task_id: null,
        source_run_id: null,
      };
      await client.executeWorkflowRunRequest("conversation-1", prepared, "step-1", "key-1");
      await client.executeGlobalWorkflowRunRequest("conversation-1", prepared, "step-2", "key-2");

      expect(requests.map((request) => request.url)).toEqual([
        "/api/internal/v1/agent-conversations/conversation-1/workflow-run-requests",
        "/api/internal/v1/agent-conversations/conversation-1/global-workflow-run-requests",
      ]);
      expect(requests[0].body).toEqual({
        expected_workflow_revision: 7,
        workflow_id: "workflow-1",
        source_step_id: "step-1",
        task_id: null,
        source_run_id: null,
        scope: "graph",
        node_id: null,
        node_ids: [],
        force: false,
        document_action: null,
      });
      expect(requests[1].body).toEqual({
        expected_workflow_revision: 7,
        workflow_id: "workflow-1",
        source_step_id: "step-2",
        task_id: null,
        source_run_id: null,
        product_id: "product-1",
        scope: "graph",
        node_id: null,
        node_ids: [],
        force: false,
        document_action: null,
      });
    } finally {
      server.closeAllConnections();
      await new Promise<void>((resolve) => server.close(() => resolve()));
    }
  });

  it("reconciles product workspace creation without issuing a create request", async () => {
    const requests: Array<{ url: string; body: Record<string, unknown>; method: string }> = [];
    const server = createServer(async (request, response) => {
      const chunks: Buffer[] = [];
      for await (const chunk of request) chunks.push(Buffer.from(chunk));
      requests.push({
        url: request.url ?? "",
        method: request.method ?? "",
        body: JSON.parse(Buffer.concat(chunks).toString("utf8")) as Record<string, unknown>,
      });
      response.writeHead(200, { "Content-Type": "application/json" });
      response.end(JSON.stringify({ state: "applied", result: { product_id: "product-1" } }));
    });
    await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
    const address = server.address();
    if (!address || typeof address === "string") throw new Error("test server did not bind");

    try {
      const client = new ProductFlowClient(`http://127.0.0.1:${address.port}`, "0123456789abcdef0123456789abcdef", 1000);
      await expect(client.reconcileProductWorkspace("conversation-1", "商品", "workspace-key")).resolves.toMatchObject({
        state: "applied",
      });
      expect(requests).toEqual([
        {
          url: "/api/internal/v1/agent-conversations/conversation-1/product-workspaces/reconcile",
          method: "POST",
          body: { name: "商品" },
        },
      ]);
    } finally {
      server.closeAllConnections();
      await new Promise<void>((resolve) => server.close(() => resolve()));
    }
  });

  it("claims, heartbeats, and releases a durable Turn execution lease", async () => {
    const requests: Array<{ url: string; body: Record<string, unknown> }> = [];
    const server = createServer(async (request, response) => {
      const chunks: Buffer[] = [];
      for await (const chunk of request) chunks.push(Buffer.from(chunk));
      const body = chunks.length > 0 ? JSON.parse(Buffer.concat(chunks).toString("utf8")) as Record<string, unknown> : {};
      requests.push({ url: request.url ?? "", body });
      response.writeHead(200, { "Content-Type": "application/json" });
      response.end(JSON.stringify(
        request.url?.endsWith("/release")
          ? { released: true }
          : request.url?.endsWith("/checkpoints")
            ? {
                id: "checkpoint-1",
                projection_id: "projection-1",
                execution_id: "execution-1",
                attempt: 1,
                fencing_token: 1,
                sequence: body.sequence ?? 1,
                kind: body.kind ?? "before_model_request",
                created_at: "2026-08-20T00:00:00.000Z",
              }
            : request.url?.endsWith("/events/confirm")
              ? {
                  status: "confirmed",
                  confirmed_through: 1,
                  persisted_through: 1,
                  items: [{
                    id: "event-1",
                    projection_id: "projection-1",
                    execution_id: "execution-1",
                    sequence: 1,
                    schema_version: 1,
                    kind: "turn/start",
                    ignorable: false,
                    created_at: "2026-08-20T00:00:00.000Z",
                  }],
                }
            : request.url?.endsWith("/events/batch")
              ? {
                  items: [{
                    id: "event-1",
                    projection_id: "projection-1",
                    execution_id: "execution-1",
                    sequence: 1,
                    schema_version: 1,
                    kind: "turn/start",
                    created_at: "2026-08-20T00:00:00.000Z",
                  }],
                }
            : {
              execution_id: "execution-1",
              projection_id: "projection-1",
              harness_turn_id: "turn-1",
              owner_id: "agent-1",
              lease_token: "lease-1",
              attempt: 1,
              fencing_token: 1,
              phase: body.phase ?? "claimed",
              lease_expires_at: "2026-08-20T00:00:00.000Z",
            },
      ));
    });
    await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
    const address = server.address();
    if (!address || typeof address === "string") throw new Error("test server did not bind");

    try {
      const client = new ProductFlowClient(`http://127.0.0.1:${address.port}`, "0123456789abcdef0123456789abcdef", 1000);
      const lease = await client.claimTurnExecution("conversation-1", {
        task_id: null,
        idempotency_key: "projection-1",
        harness_turn_id: "turn-1",
        owner_id: "agent-1",
      });
      await client.heartbeatTurnExecution("conversation-1", lease.execution_id, {
        owner_id: lease.owner_id,
        lease_token: lease.lease_token,
        phase: "tool",
      });
      await client.appendTurnCheckpoint("conversation-1", lease.execution_id, {
        owner_id: lease.owner_id,
        lease_token: lease.lease_token,
        sequence: 1,
        kind: "tool_effect_intent",
        payload: { operation: "workflow_run_request" },
      });
      await client.appendTurnEvents("conversation-1", lease.execution_id, {
        owner_id: lease.owner_id,
        lease_token: lease.lease_token,
        events: [{
          sequence: 1,
          schema_version: 1,
          run_id: "run-1",
          turn_id: "turn-1",
          kind: "turn/start",
          payload: { status: "running" },
          created_at: "2026-08-20T00:00:00.000Z",
        }],
      });
      await expect(client.confirmTurnEvents("conversation-1", lease.execution_id, {
        events: [{
          sequence: 1,
          schema_version: 1,
          run_id: "run-1",
          turn_id: "turn-1",
          kind: "turn/start",
          payload: { status: "running" },
          created_at: "2026-08-20T00:00:00.000Z",
        }],
      })).resolves.toMatchObject({ status: "confirmed", confirmed_through: 1 });
      await client.releaseTurnExecution("conversation-1", lease.execution_id, {
        owner_id: lease.owner_id,
        lease_token: lease.lease_token,
        phase: "terminal",
      });

      expect(requests.map((request) => request.url)).toEqual([
        "/api/internal/v1/agent-conversations/conversation-1/turn-executions/claim",
        "/api/internal/v1/agent-conversations/conversation-1/turn-executions/execution-1/heartbeat",
        "/api/internal/v1/agent-conversations/conversation-1/turn-executions/execution-1/checkpoints",
        "/api/internal/v1/agent-conversations/conversation-1/turn-executions/execution-1/events/batch",
        "/api/internal/v1/agent-conversations/conversation-1/turn-executions/execution-1/events/confirm",
        "/api/internal/v1/agent-conversations/conversation-1/turn-executions/execution-1/release",
      ]);
      expect(requests[1].body).toMatchObject({ owner_id: "agent-1", lease_token: "lease-1", phase: "tool" });
    } finally {
      server.closeAllConnections();
      await new Promise<void>((resolve) => server.close(() => resolve()));
    }
  });

  it("keeps the timeout active while reading an upstream response body", async () => {
    const server = createServer((_request, response) => {
      response.writeHead(200, { "Content-Type": "application/json" });
      response.write("{");
    });
    await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
    const address = server.address();
    if (!address || typeof address === "string") throw new Error("test server did not bind");

    try {
      const client = new ProductFlowClient(`http://127.0.0.1:${address.port}`, "0123456789abcdef0123456789abcdef", 30);
      await expect(client.runtimeContext("conversation", null)).rejects.toMatchObject({
        code: "timeout",
        status: 504,
      });
    } finally {
      server.closeAllConnections();
      await new Promise<void>((resolve) => server.close(() => resolve()));
    }
  });
});
