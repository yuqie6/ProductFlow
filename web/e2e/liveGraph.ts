import { expect, type APIRequestContext, type Page } from "@playwright/test";
import path from "node:path";
import { fileURLToPath } from "node:url";

export const LIVE_BROWSER_GRAPH_SWITCH = "PRODUCTFLOW_RUN_LIVE_BROWSER_GRAPH";
export const REFERENCE_PRODUCT_IMAGE = path.join(
  path.dirname(fileURLToPath(import.meta.url)),
  "fixtures",
  "reference-product.png",
);

const PNG_MAGIC = Buffer.from([0x89, 0x50, 0x4e, 0x47]);
const JPEG_MAGIC = Buffer.from([0xff, 0xd8, 0xff]);

export function requiredEnv(name: string): string {
  const value = process.env[name]?.trim();
  if (!value) {
    throw new Error(`${name} is required for the live browser graph gate`);
  }
  return value;
}

export function assertLiveBrowserGraphEnabled(): void {
  if (process.env[LIVE_BROWSER_GRAPH_SWITCH] !== "1") {
    throw new Error(
      `set ${LIVE_BROWSER_GRAPH_SWITCH}=1, start just dev, configure real prompt/image providers, then run just web-e2e-live-graph`,
    );
  }
}

export async function lockLocale(page: Page): Promise<void> {
  await page.addInitScript(() => {
    window.localStorage.setItem("productflow.locale", "zh-CN");
  });
}

export async function selectCreateImageType(page: Page, imageType: string): Promise<void> {
  const card = page.locator(`[data-image-type="${imageType}"]`);
  await expect(card).toBeVisible();
  const checkbox = card.locator('input[type="checkbox"]');
  if (!(await checkbox.isChecked())) {
    await card.click();
  }
  await expect(checkbox).toBeChecked();
}

export async function openCanvasView(page: Page): Promise<void> {
  await expect(page.locator('[aria-label="工作流画布"]')).toBeVisible();
}

export async function loginAsAdmin(page: Page, adminKey: string): Promise<void> {
  await page.goto("/login");
  const keyInput = page.getByPlaceholder("请输入管理员密钥");
  await Promise.race([
    page.waitForURL("**/products"),
    keyInput.waitFor({ state: "visible" }),
  ]);
  if (new URL(page.url()).pathname === "/login") {
    await keyInput.fill(adminKey);
    await page.getByRole("button", { name: "登录" }).click();
    await page.waitForURL("**/products");
  }
}

export async function assertRealImageProviders(request: APIRequestContext, settingsToken: string): Promise<void> {
  const unlock = await request.post("/api/settings/unlock", {
    data: { token: settingsToken },
  });
  if (!unlock.ok()) {
    throw new Error(`settings unlock failed: ${unlock.status()} ${await unlock.text()}`);
  }
  const response = await request.get("/api/settings/provider-config");
  if (!response.ok()) {
    throw new Error(`provider-config failed: ${response.status()} ${await response.text()}`);
  }
  const payload = (await response.json()) as {
    bindings: Array<{ purpose: string; provider_kind: string }>;
  };
  const prompt = payload.bindings.find((binding) => binding.purpose === "prompt");
  const image = payload.bindings.find((binding) => binding.purpose === "image");
  if (!prompt || prompt.provider_kind === "mock") {
    throw new Error("prompt purpose must be bound to a real provider, not mock");
  }
  if (!image || image.provider_kind === "mock") {
    throw new Error("image purpose must be bound to a real provider, not mock");
  }
}

export const CANVAS_DOCUMENT_SWITCH = "PRODUCTFLOW_RUN_CANVAS_DOCUMENT";

export function assertCanvasDocumentEnabled(): void {
  if (process.env[CANVAS_DOCUMENT_SWITCH] !== "1") {
    throw new Error(
      `set ${CANVAS_DOCUMENT_SWITCH}=1, start just dev, bind mock prompt/image providers, then run just web-e2e-canvas-document`,
    );
  }
}

export async function assertMockImageProviders(request: APIRequestContext, settingsToken: string): Promise<void> {
  const unlock = await request.post("/api/settings/unlock", {
    data: { token: settingsToken },
  });
  if (!unlock.ok()) {
    throw new Error(`settings unlock failed: ${unlock.status()} ${await unlock.text()}`);
  }
  const response = await request.get("/api/settings/provider-config");
  if (!response.ok()) {
    throw new Error(`provider-config failed: ${response.status()} ${await response.text()}`);
  }
  const payload = (await response.json()) as {
    bindings: Array<{ purpose: string; provider_kind: string }>;
  };
  const prompt = payload.bindings.find((binding) => binding.purpose === "prompt");
  const image = payload.bindings.find((binding) => binding.purpose === "image");
  if (!prompt || prompt.provider_kind !== "mock") {
    throw new Error("prompt purpose must be bound to mock for the canvas document gate");
  }
  if (!image || image.provider_kind !== "mock") {
    throw new Error("image purpose must be bound to mock for the canvas document gate");
  }
}

export async function waitForGraphRunSucceeded(
  request: APIRequestContext,
  productId: string,
  graphId: string,
): Promise<void> {
  await expect
    .poll(
      async () => {
        const response = await request.get(
          `/api/v3/products/${encodeURIComponent(productId)}/workflows/${encodeURIComponent(graphId)}/runs`,
        );
        if (!response.ok()) {
          return `http ${response.status()}`;
        }
        const payload = (await response.json()) as {
          items: Array<{ status: string; failure_reason: string | null }>;
        };
        const run = payload.items[0];
        if (!run) return "missing";
        if (run.status === "failed" || run.status === "cancelled" || run.status === "unknown") {
          throw new Error(`graph run ${run.status}: ${run.failure_reason ?? "no failure reason"}`);
        }
        return run.status;
      },
      {
        timeout: 12 * 60 * 1000,
        intervals: [2_000, 3_000, 5_000],
      },
    )
    .toBe("succeeded");
}

export async function assertGeneratedImageBytes(
  request: APIRequestContext,
  productId: string,
): Promise<{ id: string; downloadUrl: string }> {
  const response = await request.get(
    `/api/v2/products/${encodeURIComponent(productId)}/image-assets?directory_kind=generated&sort=created_desc&limit=20`,
  );
  if (!response.ok()) {
    throw new Error(`generated assets failed: ${response.status()} ${await response.text()}`);
  }
  const payload = (await response.json()) as {
    items: Array<{
      id: string;
      origin_type: string;
      byte_size: number | null;
      download_url: string;
      generation: unknown;
    }>;
  };
  const generated = payload.items.find((item) => item.origin_type === "workflow_generation" && item.generation);
  if (!generated) {
    throw new Error("no workflow_generation asset in the product library");
  }
  const media = await request.get(generated.download_url);
  if (!media.ok()) {
    throw new Error(`generated media failed: ${media.status()} ${generated.download_url}`);
  }
  const body = Buffer.from(await media.body());
  const imageLike = body.subarray(0, 4).equals(PNG_MAGIC)
    || body.subarray(0, 3).equals(JPEG_MAGIC)
    || body.subarray(8, 12).toString("ascii") === "WEBP";
  if (!imageLike || body.length < 1024) {
    throw new Error(`generated media is not a real image: ${body.length} bytes`);
  }
  if (generated.byte_size != null && generated.byte_size < 1024) {
    throw new Error(`generated asset byte_size is too small: ${generated.byte_size}`);
  }
  return { id: generated.id, downloadUrl: generated.download_url };
}
