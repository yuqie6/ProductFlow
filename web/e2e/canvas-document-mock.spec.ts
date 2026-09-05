import { expect, test, type Page } from "@playwright/test";
import { readdirSync, readFileSync } from "node:fs";
import path from "node:path";

import {
  CANVAS_DOCUMENT_SWITCH,
  REFERENCE_PRODUCT_IMAGE,
  lockLocale,
  loginAsAdmin,
  openCanvasView,
  requiredEnv,
  selectCreateImageType,
  waitForGraphRunSucceeded,
  withMockDocumentProviders,
} from "./liveGraph";

interface GraphNodePayload {
  id: string;
  node_type: string;
  document_origin?: string | null;
  pending_candidate_artifact_id?: string | null;
  config: {
    prompt?: {
      design_goal?: string;
      composition?: { layout?: string; product_share_percent?: number };
    };
  };
}

interface GraphPayload {
  id: string;
  revision: number;
  nodes: GraphNodePayload[];
}

interface GraphRunListPayload {
  items: Array<{ status: string }>;
}

function findGraphWorkerPids(): number[] {
  const isolatedPID = process.env.PRODUCTFLOW_GRAPH_WORKER_PID;
  if (isolatedPID) {
    const pid = Number(isolatedPID);
    if (!Number.isSafeInteger(pid) || pid < 1) throw new Error("invalid isolated worker PID");
    const cmd = readFileSync(`/proc/${pid}/cmdline`, "utf8");
    if (!cmd.includes("productflow-worker")) throw new Error("isolated PID is not a graph worker");
    return [pid];
  }
  const pids: number[] = [];
  for (const entry of readdirSync("/proc")) {
    if (!/^\d+$/.test(entry)) continue;
    try {
      const cmd = readFileSync(path.join("/proc", entry, "cmdline"), "utf8").replace(/\0/g, " ");
      if (cmd.includes("productflow-worker") || cmd.includes("cmd/productflow-worker")) {
        pids.push(Number(entry));
      }
    } catch {
      continue;
    }
  }
  return pids;
}

async function withPausedGraphWorker<T>(run: () => Promise<T>): Promise<T> {
  const pids = findGraphWorkerPids();
  expect(pids.length, "just dev 的 productflow-worker 必须在跑").toBeGreaterThan(0);
  for (const pid of pids) process.kill(pid, "SIGSTOP");
  try {
    return await run();
  } finally {
    for (const pid of pids) {
      try {
        process.kill(pid, "SIGCONT");
      } catch {
        // worker 可能已经退出
      }
    }
  }
}

async function latestRunStatus(page: Page, productID: string, graphID: string): Promise<string | null> {
  const response = await page.request.get(
    `/api/v3/products/${encodeURIComponent(productID)}/workflows/${encodeURIComponent(graphID)}/runs`,
  );
  expect(response.ok(), await response.text()).toBeTruthy();
  const payload = await response.json() as GraphRunListPayload;
  return payload.items[0]?.status ?? null;
}

function isGraphRunSubmit(request: { method: () => string; url: () => string }): boolean {
  if (request.method() !== "POST") return false;
  const url = new URL(request.url());
  return /\/runs$/.test(url.pathname) && !url.pathname.includes("/preview");
}

async function createWorkbench(page: Page): Promise<string> {
  await page.goto("/products/new");
  await page.locator("#agent-product-name").fill(`canvas-document ${Date.now()}`);
  await page.locator("#agent-product-brief").fill("文稿改写候选验证，使用 mock 供应商。");
  await selectCreateImageType(page, "detail");
  await page.locator('[data-image-type="detail"] input[type="number"]').fill("1");
  await page.locator("[data-agent-product-intake-form] input[type='file']").setInputFiles(REFERENCE_PRODUCT_IMAGE);
  await expect(page.locator("[data-create-direct]")).toHaveAttribute("data-create-direct-ready", "true");
  await Promise.all([
    page.waitForURL(/\/products\/(?!new(?:\/|$))[^/]+$/),
    page.locator("[data-create-direct]").click(),
  ]);
  await expect(page.locator("[data-graph-canvas-panel]")).toBeVisible();
  await openCanvasView(page);
  return new URL(page.url()).pathname.split("/").filter(Boolean).at(-1) ?? "";
}

async function currentGraph(page: Page, productID: string): Promise<GraphPayload> {
  const response = await page.request.get(`/api/v3/products/${encodeURIComponent(productID)}/workflows/current`);
  expect(response.ok(), await response.text()).toBeTruthy();
  return await response.json() as GraphPayload;
}

async function authorPrompt(page: Page, productID: string): Promise<GraphNodePayload> {
  const graph = await currentGraph(page, productID);
  const prompt = graph.nodes.find((node) => node.node_type === "image_prompt");
  expect(prompt).toBeTruthy();
  await selectNode(page, prompt!.id);
  await page.getByLabel("设计目标").fill("人工目标-不要被构图应用改掉");
  await page.getByLabel("商品近似占比（%）").fill("55");
  await page.getByLabel("布局").fill("人工左侧留白");
  await expect.poll(async () => {
    const latest = await currentGraph(page, productID);
    const authored = latest.nodes.find((node) => node.id === prompt!.id);
    return authored?.document_origin === "authored"
      && authored.config.prompt?.design_goal === "人工目标-不要被构图应用改掉"
      && authored.config.prompt?.composition?.layout === "人工左侧留白"
      && authored.config.prompt?.composition?.product_share_percent === 55;
  }).toBeTruthy();
  const after = await currentGraph(page, productID);
  return after.nodes.find((node) => node.id === prompt!.id)!;
}

async function selectNode(
  page: Page,
  nodeID: string,
  ready: () => Promise<boolean> = () => page.getByLabel("设计目标").isVisible(),
): Promise<void> {
  await page.locator('[data-sidebar-tool="details"]').click();
  const card = page.locator(`[data-workflow-node-id="${nodeID}"]`);
  await expect(card).toBeVisible();
  await expect.poll(async () => {
    await card.evaluate((element: HTMLElement) => {
      element.dispatchEvent(new MouseEvent("click", {
        bubbles: true,
        cancelable: true,
        view: window,
        buttons: 1,
      }));
    });
    return ready();
  }).toBeTruthy();
}

test.describe("canvas document mock provider", () => {
  test.skip(process.env[CANVAS_DOCUMENT_SWITCH] !== "1", `set ${CANVAS_DOCUMENT_SWITCH}=1`);

  test("rewrite stages a candidate and section apply keeps unselected fields", async ({ page }) => {
    await lockLocale(page);
    await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
    await withMockDocumentProviders(page.request, requiredEnv("SETTINGS_ACCESS_TOKEN"), async () => {
    const productID = await createWorkbench(page);
    const prompt = await authorPrompt(page, productID);
    await page.reload();
    await openCanvasView(page);
    await selectNode(page, prompt.id);
    await expect(page.locator("[data-graph-inspector-rewrite]")).toBeEnabled();
    const graph = await currentGraph(page, productID);
    await page.locator("[data-graph-inspector-rewrite]").click();
    await waitForGraphRunSucceeded(page.request, productID, graph.id);
    await expect.poll(async () => {
      const latest = await currentGraph(page, productID);
      return latest.nodes.find((node) => node.id === prompt.id)?.pending_candidate_artifact_id ?? null;
    }).not.toBeNull();
    const liveBeforeApply = await currentGraph(page, productID);
    const beforeNode = liveBeforeApply.nodes.find((node) => node.id === prompt.id);
    expect(beforeNode?.config.prompt?.design_goal).toBe("人工目标-不要被构图应用改掉");
    expect(beforeNode?.config.prompt?.composition?.layout).toBe("人工左侧留白");

    await selectNode(page, prompt.id);
    const review = page.locator("[data-graph-document-candidate]");
    await expect(review).toBeVisible();
    const composition = review.getByRole("button", { name: "构图" });
    await expect(composition).toBeVisible();
    if ((await composition.getAttribute("aria-pressed")) !== "true") {
      await composition.click();
    }
    const objective = review.getByRole("button", { name: "目标" });
    if (await objective.isVisible() && (await objective.getAttribute("aria-pressed")) === "true") {
      await objective.click();
    }
    await review.getByRole("button", { name: "应用所选" }).click();
    await expect.poll(async () => {
      const latest = await currentGraph(page, productID);
      return latest.nodes.find((node) => node.id === prompt.id)?.pending_candidate_artifact_id ?? null;
    }).toBeNull();
    const applied = await currentGraph(page, productID);
    const appliedNode = applied.nodes.find((node) => node.id === prompt.id);
    expect(appliedNode?.config.prompt?.design_goal).toBe("人工目标-不要被构图应用改掉");
    expect(appliedNode?.config.prompt?.composition?.layout).not.toBe("人工左侧留白");
    });
  });

  test("complete and replace stage candidates without writing live config", async ({ page }) => {
    await lockLocale(page);
    await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
    await withMockDocumentProviders(page.request, requiredEnv("SETTINGS_ACCESS_TOKEN"), async () => {
    const productID = await createWorkbench(page);
    const prompt = await authorPrompt(page, productID);
    await page.reload();
    await openCanvasView(page);
    await selectNode(page, prompt.id);

    await page.locator("[data-graph-inspector-complete]").click();
    const graph = await currentGraph(page, productID);
    await waitForGraphRunSucceeded(page.request, productID, graph.id);
    await expect.poll(async () => {
      const latest = await currentGraph(page, productID);
      return latest.nodes.find((node) => node.id === prompt.id)?.pending_candidate_artifact_id ?? null;
    }).not.toBeNull();
    const afterComplete = await currentGraph(page, productID);
    const completeNode = afterComplete.nodes.find((node) => node.id === prompt.id);
    expect(completeNode?.config.prompt?.design_goal).toBe("人工目标-不要被构图应用改掉");
    expect(completeNode?.config.prompt?.composition?.layout).toBe("人工左侧留白");

    await selectNode(page, prompt.id);
    await page.locator("[data-graph-document-candidate]").getByRole("button", { name: "放弃建议" }).click();
    await expect.poll(async () => {
      const latest = await currentGraph(page, productID);
      return latest.nodes.find((node) => node.id === prompt.id)?.pending_candidate_artifact_id ?? null;
    }).toBeNull();

    await selectNode(page, prompt.id);
    await page.locator("[data-graph-inspector-replace]").click();
    await page.getByRole("dialog").getByRole("button", { name: "重新生成" }).click();
    await waitForGraphRunSucceeded(page.request, productID, graph.id);
    await expect.poll(async () => {
      const latest = await currentGraph(page, productID);
      return latest.nodes.find((node) => node.id === prompt.id)?.pending_candidate_artifact_id ?? null;
    }).not.toBeNull();
    const afterReplace = await currentGraph(page, productID);
    const replaceNode = afterReplace.nodes.find((node) => node.id === prompt.id);
    expect(replaceNode?.config.prompt?.design_goal).toBe("人工目标-不要被构图应用改掉");
    expect(replaceNode?.config.prompt?.composition?.layout).toBe("人工左侧留白");
    });
  });

  test("mid-run inspector typing keeps live authored copy after graph adopt", async ({ page }) => {
    await lockLocale(page);
    await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
    await withMockDocumentProviders(page.request, requiredEnv("SETTINGS_ACCESS_TOKEN"), async () => {
      const productID = await createWorkbench(page);
      const graph = await currentGraph(page, productID);
      const prompt = graph.nodes.find((node) => node.node_type === "image_prompt");
      expect(prompt).toBeTruthy();
      await selectNode(page, prompt!.id);
      await expect(page.getByLabel("设计目标")).toBeVisible();
      const authoredGoal = `运行中手填-${Date.now()}`;
      let savedWhileInFlight: string | null = null;

      await withPausedGraphWorker(async () => {
        const runPosted = page.waitForRequest((request) => {
          if (request.method() !== "POST") return false;
          const url = new URL(request.url());
          return /\/runs$/.test(url.pathname) && !url.pathname.includes("/preview");
        });
        await page.locator("[data-graph-run-all]").click();
        const posted = await runPosted;
        expect(JSON.parse(posted.postData() ?? "{}")).toMatchObject({ scope: "graph" });
        await expect.poll(async () => latestRunStatus(page, productID, graph.id)).toMatch(/^(running|queued)$/);
        await selectNode(page, prompt!.id);
        await page.getByRole("button", { name: "详情" }).click();
        await expect(page.getByLabel("设计目标")).toBeVisible();
        await page.getByLabel("设计目标").fill(authoredGoal);
        await page.getByLabel("商品近似占比（%）").fill("55");
        await page.getByLabel("布局").fill("运行中左侧留白");
        await expect.poll(async () => {
          const latest = await currentGraph(page, productID);
          const authored = latest.nodes.find((node) => node.id === prompt!.id);
          return authored?.config.prompt?.design_goal === authoredGoal
            && authored.config.prompt?.composition?.layout === "运行中左侧留白"
            && authored.config.prompt?.composition?.product_share_percent === 55;
        }).toBeTruthy();
        savedWhileInFlight = await latestRunStatus(page, productID, graph.id);
      });

      expect(savedWhileInFlight).toMatch(/^(running|queued)$/);
      await waitForGraphRunSucceeded(page.request, productID, graph.id);
      const after = await currentGraph(page, productID);
      const afterNode = after.nodes.find((node) => node.id === prompt!.id);
      expect(afterNode?.config.prompt?.design_goal).toBe(authoredGoal);
      expect(afterNode?.config.prompt?.composition?.layout).toBe("运行中左侧留白");
      expect(afterNode?.config.prompt?.composition?.product_share_percent).toBe(55);
      expect(afterNode?.document_origin).toBe("authored");
    });
  });

  test("mid-run undo keeps reverted live copy after graph adopt", async ({ page }) => {
    await lockLocale(page);
    await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
    await withMockDocumentProviders(page.request, requiredEnv("SETTINGS_ACCESS_TOKEN"), async () => {
      const productID = await createWorkbench(page);
      const graph = await currentGraph(page, productID);
      const prompt = graph.nodes.find((node) => node.node_type === "image_prompt");
      expect(prompt).toBeTruthy();
      const seedGoal = prompt!.config.prompt?.design_goal ?? null;
      const authored = await authorPrompt(page, productID);
      expect(authored.config.prompt?.design_goal).toBe("人工目标-不要被构图应用改掉");
      let undoneWhileInFlight: string | null = null;

      await withPausedGraphWorker(async () => {
        const runPosted = page.waitForRequest((request) => {
          if (request.method() !== "POST") return false;
          const url = new URL(request.url());
          return /\/runs$/.test(url.pathname) && !url.pathname.includes("/preview");
        });
        await page.locator("[data-graph-run-all]").click();
        const posted = await runPosted;
        expect(JSON.parse(posted.postData() ?? "{}")).toMatchObject({ scope: "graph" });
        await expect.poll(async () => latestRunStatus(page, productID, graph.id)).toMatch(/^(running|queued)$/);
        const undo = page.locator("[data-graph-canvas-toolbar]").getByLabel("撤销");
        await expect(undo).toBeEnabled();
        await undo.click();
        await expect.poll(async () => {
          const latest = await currentGraph(page, productID);
          return latest.nodes.find((node) => node.id === prompt!.id)?.config.prompt?.design_goal ?? null;
        }).toBe(seedGoal);
        undoneWhileInFlight = await latestRunStatus(page, productID, graph.id);
      });

      expect(undoneWhileInFlight).toMatch(/^(running|queued)$/);
      await waitForGraphRunSucceeded(page.request, productID, graph.id);
      const after = await currentGraph(page, productID);
      const afterNode = after.nodes.find((node) => node.id === prompt!.id);
      expect(afterNode?.config.prompt?.design_goal).toBe(seedGoal);
      expect(afterNode?.config.prompt?.design_goal).not.toBe("人工目标-不要被构图应用改掉");
      expect(afterNode?.config.prompt?.composition?.layout).not.toBe("人工左侧留白");
    });
  });

  test("document save 409 shows revision conflict and does not replay", async ({ page }) => {
    await lockLocale(page);
    await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
    await withMockDocumentProviders(page.request, requiredEnv("SETTINGS_ACCESS_TOKEN"), async () => {
      const productID = await createWorkbench(page);
      const graph = await currentGraph(page, productID);
      const prompt = graph.nodes.find((node) => node.node_type === "image_prompt");
      expect(prompt).toBeTruthy();
      await selectNode(page, prompt!.id);
      await expect(page.getByLabel("设计目标")).toBeVisible();

      const documentSaves: Array<{ status: number }> = [];
      page.on("response", (response) => {
        if (response.request().method() !== "POST") return;
        const url = new URL(response.url());
        if (!url.pathname.endsWith("/changesets")) return;
        let body: { operations?: Array<{ op?: string }> } = {};
        try {
          body = response.request().postDataJSON() as typeof body;
        } catch {
          return;
        }
        if (!(body.operations ?? []).some((operation) => operation.op === "update_node_config")) return;
        documentSaves.push({ status: response.status() });
      });

      const dirtyGoal = `冲突草稿-${Date.now()}`;
      await page.getByLabel("设计目标").fill(dirtyGoal);
      const beforeConflict = await currentGraph(page, productID);
      const liveNode = beforeConflict.nodes.find((node) => node.id === prompt!.id);
      expect(liveNode).toBeTruthy();
      const config = JSON.parse(JSON.stringify(liveNode!.config)) as {
        prompt?: { composition?: { layout?: string } };
      };
      if (!config.prompt) config.prompt = {};
      if (!config.prompt.composition) config.prompt.composition = {};
      config.prompt.composition.layout = `服务端同节点-${Date.now()}`;
      const patched = await page.request.post(
        `/api/v3/products/${encodeURIComponent(productID)}/workflows/${encodeURIComponent(beforeConflict.id)}/changesets`,
        {
          data: {
            base_graph_revision: beforeConflict.revision,
            summary: "e2e 同节点冲突",
            operations: [{ op: "update_node_config", node_ref: prompt!.id, config }],
          },
        },
      );
      expect(patched.ok(), await patched.text()).toBeTruthy();

      await expect.poll(() => documentSaves.some((save) => save.status === 409)).toBeTruthy();
      await expect(page.locator("[data-graph-canvas-notice]")).toContainText(
        "画布版本已更新，这次修改无法自动合并，请重做",
      );
      const conflictIndex = documentSaves.findIndex((save) => save.status === 409);
      const afterConflict = Date.now() + 1600;
      await expect.poll(() => Date.now() >= afterConflict).toBeTruthy();
      expect(documentSaves.filter((save) => save.status === 409)).toHaveLength(1);
      expect(documentSaves.slice(conflictIndex + 1)).toEqual([]);
    });
  });

  test("inspector run-this-node keeps authored live copy", async ({ page }) => {
    await lockLocale(page);
    await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
    await withMockDocumentProviders(page.request, requiredEnv("SETTINGS_ACCESS_TOKEN"), async () => {
      const productID = await createWorkbench(page);
      const authored = await authorPrompt(page, productID);
      const graph = await currentGraph(page, productID);
      const runPosted = page.waitForRequest(isGraphRunSubmit);
      await page.locator("[data-graph-inspector-run-node]").click();
      const posted = await runPosted;
      expect(JSON.parse(posted.postData() ?? "{}")).toMatchObject({
        scope: "node",
        node_id: authored.id,
      });
      expect(JSON.parse(posted.postData() ?? "{}").force).toBeUndefined();
      await waitForGraphRunSucceeded(page.request, productID, graph.id);
      const after = await currentGraph(page, productID);
      const afterNode = after.nodes.find((node) => node.id === authored.id);
      expect(afterNode?.config.prompt?.design_goal).toBe("人工目标-不要被构图应用改掉");
      expect(afterNode?.config.prompt?.composition?.layout).toBe("人工左侧留白");
      expect(afterNode?.document_origin).toBe("authored");
    });
  });

  test("inspector run-to-here keeps authored prompt live copy", async ({ page }) => {
    await lockLocale(page);
    await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
    await withMockDocumentProviders(page.request, requiredEnv("SETTINGS_ACCESS_TOKEN"), async () => {
      const productID = await createWorkbench(page);
      const authored = await authorPrompt(page, productID);
      const graph = await currentGraph(page, productID);
      const image = graph.nodes.find((node) => node.node_type === "image_generation");
      expect(image).toBeTruthy();
      await selectNode(page, image!.id, () => page.locator("[data-graph-inspector-run-to-node]").isVisible());
      const runPosted = page.waitForRequest(isGraphRunSubmit);
      await page.locator("[data-graph-inspector-run-to-node]").click();
      const posted = await runPosted;
      expect(JSON.parse(posted.postData() ?? "{}")).toMatchObject({
        scope: "to_node",
        node_id: image!.id,
      });
      expect(JSON.parse(posted.postData() ?? "{}").force).toBeUndefined();
      await waitForGraphRunSucceeded(page.request, productID, graph.id);
      const after = await currentGraph(page, productID);
      const afterNode = after.nodes.find((node) => node.id === authored.id);
      expect(afterNode?.config.prompt?.design_goal).toBe("人工目标-不要被构图应用改掉");
      expect(afterNode?.config.prompt?.composition?.layout).toBe("人工左侧留白");
      expect(afterNode?.document_origin).toBe("authored");
    });
  });

  test("mid-run cancel keeps authored live copy", async ({ page }) => {
    await lockLocale(page);
    await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
    await withMockDocumentProviders(page.request, requiredEnv("SETTINGS_ACCESS_TOKEN"), async () => {
      const productID = await createWorkbench(page);
      const authored = await authorPrompt(page, productID);
      const graph = await currentGraph(page, productID);
      let cancelledFrom: string | null = null;

      await withPausedGraphWorker(async () => {
        const runPosted = page.waitForRequest(isGraphRunSubmit);
        await page.locator("[data-graph-run-all]").click();
        const posted = await runPosted;
        expect(JSON.parse(posted.postData() ?? "{}")).toMatchObject({ scope: "graph" });
        await expect.poll(async () => latestRunStatus(page, productID, graph.id)).toMatch(/^(running|queued)$/);
        const queuedCancel = page.locator("[data-graph-run-queue]").getByLabel("取消排队");
        const inspectorCancel = page.locator("[data-graph-inspector-cancel]");
        const inFlight = await latestRunStatus(page, productID, graph.id);
        expect(inFlight).toMatch(/^(running|queued)$/);
        if (await queuedCancel.isVisible()) {
          await queuedCancel.click();
          cancelledFrom = "queued";
        } else {
          await expect(inspectorCancel).toBeVisible();
          await inspectorCancel.click();
          cancelledFrom = "inspector";
        }
        await expect.poll(async () => latestRunStatus(page, productID, graph.id)).toBe("cancelled");
      });

      expect(cancelledFrom).toMatch(/^(queued|inspector)$/);
      const after = await currentGraph(page, productID);
      const afterNode = after.nodes.find((node) => node.id === authored.id);
      expect(afterNode?.config.prompt?.design_goal).toBe("人工目标-不要被构图应用改掉");
      expect(afterNode?.config.prompt?.composition?.layout).toBe("人工左侧留白");
      expect(afterNode?.document_origin).toBe("authored");
    });
  });
});
