import { expect, test, type Page } from "@playwright/test";

import { lockLocale, loginAsAdmin, requiredEnv } from "./liveGraph";

const PERF_SWITCH = "PRODUCTFLOW_RUN_WORKBENCH_PERF";
const DEFAULT_TTI_BUDGET_MS = 3_000;

function enabled(): boolean {
  return process.env[PERF_SWITCH] === "1";
}

function productId(): string {
  return requiredEnv("PRODUCTFLOW_PERF_PRODUCT_ID");
}

function ttiBudget(): number {
  const raw = Number(process.env.PRODUCTFLOW_WORKBENCH_TTI_BUDGET_MS ?? DEFAULT_TTI_BUDGET_MS);
  return Number.isFinite(raw) && raw > 0 ? raw : DEFAULT_TTI_BUDGET_MS;
}

function isCurrentGraphPath(pathname: string): boolean {
  return /\/api\/v3\/products\/[^/]+\/workflows\/current$/.test(pathname);
}

function isRunListPath(pathname: string): boolean {
  return /\/api\/v3\/products\/[^/]+\/workflows\/[^/]+\/runs$/.test(pathname);
}

function isRunDetailPath(pathname: string): boolean {
  return /\/api\/v3\/products\/[^/]+\/workflows\/[^/]+\/runs\/[^/]+$/.test(pathname);
}

function isGraphSSEPath(pathname: string): boolean {
  return /\/api\/v3\/products\/[^/]+\/workflows\/[^/]+\/runs\/[^/]+\/events$/.test(pathname);
}

async function measureWorkbenchNavigation(page: Page, id: string, reload: boolean): Promise<{
  elapsedMs: number;
  requests: string[];
}> {
  const requests: string[] = [];
  const onRequest = (request: { method(): string; url(): string }) => {
    if (request.method() === "GET") requests.push(new URL(request.url()).pathname);
  };
  page.on("request", onRequest);
  const startedAt = Date.now();
  if (reload) {
    await page.reload({ waitUntil: "domcontentloaded" });
  } else {
    await page.goto(`/products/${encodeURIComponent(id)}`, { waitUntil: "domcontentloaded" });
  }
  await page.locator("[data-agent-workbench-shell]").waitFor({ state: "visible" });
  await page.locator("[data-graph-canvas-panel]").waitFor({ state: "visible" });
  // Allow query observers mounted by the workbench to settle before taking request counts.
  await page.waitForTimeout(300);
  const elapsedMs = Date.now() - startedAt;
  page.off("request", onRequest);
  return { elapsedMs, requests };
}

function countRequests(requests: string[], predicate: (pathname: string) => boolean): number {
  return requests.filter(predicate).length;
}

test.describe("workbench performance contract", () => {
  test.beforeEach(async ({ page }) => {
    test.skip(!enabled(), `set ${PERF_SWITCH}=1 to run the browser performance gate`);
    await lockLocale(page);
    await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));
  });

  test("keeps initial workbench TTI and read projections bounded", async ({ page }) => {
    const id = productId();
    const consoleErrors: string[] = [];
    const pageErrors: string[] = [];
    const requestFailures: string[] = [];
    const httpFailures: string[] = [];
    page.on("console", (message) => {
      if (message.type() === "error") consoleErrors.push(message.text());
    });
    page.on("pageerror", (error) => pageErrors.push(error.message));
    page.on("requestfailed", (request) => {
      const errorText = request.failure()?.errorText ?? "";
      if (!errorText.includes("ERR_ABORTED")) requestFailures.push(`${request.url()} ${errorText}`);
    });
    page.on("response", (response) => {
      const status = response.status();
      const pathname = new URL(response.url()).pathname;
      const expectedAgentBootstrapConflict = status === 409 && pathname.endsWith("/agent-workbench");
      if (status >= 400 && !expectedAgentBootstrapConflict) {
        httpFailures.push(`${status} ${pathname}`);
      }
    });
    const cold = await measureWorkbenchNavigation(page, id, false);
    const warm = await measureWorkbenchNavigation(page, id, true);
    const allInitialDetailRequests = [
      ...cold.requests.filter(isRunDetailPath),
      ...warm.requests.filter(isRunDetailPath),
    ];

    expect(cold.elapsedMs, `cold workbench TTI exceeded ${ttiBudget()}ms`).toBeLessThanOrEqual(ttiBudget());
    expect(warm.elapsedMs, `warm workbench TTI exceeded ${ttiBudget()}ms`).toBeLessThanOrEqual(ttiBudget());
    expect(countRequests(cold.requests, isCurrentGraphPath), `cold current graph request duplicated: ${cold.requests.join(" ")}`).toBeLessThanOrEqual(1);
    expect(countRequests(warm.requests, isCurrentGraphPath), `warm current graph request duplicated: ${warm.requests.join(" ")}`).toBeLessThanOrEqual(1);
    expect(countRequests(cold.requests, isRunListPath), `cold run list request duplicated: ${cold.requests.join(" ")}`).toBeLessThanOrEqual(1);
    expect(countRequests(warm.requests, isRunListPath), `warm run list request duplicated: ${warm.requests.join(" ")}`).toBeLessThanOrEqual(1);
    expect(countRequests(cold.requests, isGraphSSEPath), "cold Graph SSE connection duplicated").toBeLessThanOrEqual(1);
    expect(countRequests(warm.requests, isGraphSSEPath), "warm Graph SSE connection duplicated").toBeLessThanOrEqual(1);
    expect(allInitialDetailRequests, "rich run detail must stay closed on initial workbench load").toHaveLength(0);

    let inspectorDetailRequests = 0;
    const technicalDetails = page.locator("[data-graph-technical-details]").first();
    if (await technicalDetails.count()) {
      const explicitRequests: string[] = [];
      const onRequest = (request: { method(): string; url(): string }) => {
        if (request.method() === "GET") explicitRequests.push(new URL(request.url()).pathname);
      };
      page.on("request", onRequest);
      await technicalDetails.locator(":scope > summary").click();
      await page.waitForTimeout(300);
      page.off("request", onRequest);
      // The opened disclosure may have no historical node run. In either case it may issue
      // at most one rich read for the selected run and never one read per rendered field.
      inspectorDetailRequests = countRequests(explicitRequests, isRunDetailPath);
      expect(inspectorDetailRequests).toBeLessThanOrEqual(1);
    }

    let visibleRunCount = 0;
    let openedRunDetailRequests = 0;
    const runsTab = page.locator('[data-sidebar-tool="runs"]');
    if (await runsTab.count()) {
      await runsTab.click();
      const runsPanel = page.locator("[data-graph-runs-panel]");
      await expect(runsPanel).toBeVisible();
      const runCards = runsPanel.locator("[data-graph-run-id]");
      visibleRunCount = await runCards.count();
      if (visibleRunCount > 0) {
        const firstRun = runCards.first();
        const runId = await firstRun.getAttribute("data-graph-run-id");
        expect(runId).toBeTruthy();
        const explicitRequests: string[] = [];
        const onRequest = (request: { method(): string; url(): string }) => {
          if (request.method() === "GET") explicitRequests.push(new URL(request.url()).pathname);
        };
        page.on("request", onRequest);
        await firstRun.locator("[data-graph-run-details-toggle]").click();
        await expect(firstRun.locator(`[data-graph-run-details="${runId}"]`)).toBeVisible();
        await page.waitForTimeout(300);
        page.off("request", onRequest);
        openedRunDetailRequests = countRequests(explicitRequests, isRunDetailPath);
        expect(openedRunDetailRequests).toBeLessThanOrEqual(1);
      }
    }

    const unexpectedConsoleErrors = consoleErrors.filter((message) => !message.includes("status of 409 (Conflict)"));
    expect(unexpectedConsoleErrors, "unexpected browser console errors").toEqual([]);
    expect(pageErrors, "browser page errors").toEqual([]);
    expect(requestFailures, "browser network failures").toEqual([]);
    expect(httpFailures, "unexpected HTTP failures").toEqual([]);

    console.log(JSON.stringify({
      cold_tti_ms: cold.elapsedMs,
      warm_tti_ms: warm.elapsedMs,
      cold_current_graph_requests: countRequests(cold.requests, isCurrentGraphPath),
      warm_current_graph_requests: countRequests(warm.requests, isCurrentGraphPath),
      cold_run_list_requests: countRequests(cold.requests, isRunListPath),
      warm_run_list_requests: countRequests(warm.requests, isRunListPath),
      cold_graph_sse_connections: countRequests(cold.requests, isGraphSSEPath),
      warm_graph_sse_connections: countRequests(warm.requests, isGraphSSEPath),
      initial_run_detail_requests: allInitialDetailRequests.length,
      inspector_detail_requests_after_open: inspectorDetailRequests,
      visible_run_count: visibleRunCount,
      run_detail_requests_after_open: openedRunDetailRequests,
      console_error_count: consoleErrors.length,
      page_error_count: pageErrors.length,
      network_failure_count: requestFailures.length,
      http_failure_count: httpFailures.length,
      tti_budget_ms: ttiBudget(),
      product_id: id,
    }));
  });
});
