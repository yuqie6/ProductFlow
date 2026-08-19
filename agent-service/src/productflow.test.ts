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
      });
      expect(requests[1].body).toEqual({
        expected_workflow_revision: 7,
        workflow_id: "workflow-1",
        source_step_id: "step-2",
        task_id: null,
        source_run_id: null,
        product_id: "product-1",
      });
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
