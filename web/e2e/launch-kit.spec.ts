import { expect, test, type Page } from "@playwright/test";

async function installClipboardStub(page: Page) {
  await page.addInitScript(() => {
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText: async () => undefined },
    });
  });
}

async function login(page: Page) {
  await page.goto("/");
  await expect(page).toHaveURL(/\/login$/);
  await page.getByPlaceholder("请输入管理员密钥").fill("super-secret-admin-key");
  await page.getByRole("button", { name: /登录|Log in|Đăng nhập/i }).click();
  await expect(page).toHaveURL(/\/launch-kits$/);
}

async function createLaunchKit(page: Page, name: string, options?: { referenceText?: string; notes?: string }) {
  await page.goto("/launch-kits/new");
  await page.getByLabel("Tên sản phẩm").fill(name);
  await page.getByLabel("Thông tin tham khảo").fill(options?.referenceText ?? "Vải nhẹ, nhiều màu, size M L XL, dùng khi đi xe máy.");
  await page.getByLabel("URL tham khảo").fill("https://example.com/ao-khoac");
  await page.getByLabel("Ghi chú người bán").fill(options?.notes ?? "Tone rõ ràng, tránh claim y tế.");
  await page.getByRole("button", { name: "Tạo LaunchKit" }).click();
  await expect(page).toHaveURL(/\/launch-kits\/[a-f0-9-]+$/);
  await expect(page.getByRole("heading", { name })).toBeVisible();
}

test.beforeEach(async ({ page }) => {
  await installClipboardStub(page);
});

test("seller can create, generate, copy, export, give feedback, and reach Advanced mode", async ({ page }) => {
  await login(page);
  await expect(page.getByRole("heading", { name: /Shopee \/ TikTok Shop LaunchKit/i })).toBeVisible();

  await createLaunchKit(page, "Áo khoác chống nắng UPF50");

  await page.getByRole("button", { name: "Tạo nội dung" }).click();
  await expect(page.locator("text=/ready/i").first()).toBeVisible();
  await expect(page.getByRole("heading", { name: "Proof-first practical value" })).toBeVisible();
  await expect(page.getByText("/ 100 sẵn sàng")).toBeVisible();

  await page.getByRole("button", { name: "Copy tiêu đề" }).first().click();
  await expect(page.getByRole("button", { name: "Đã copy" }).first()).toBeVisible();
  await page.getByRole("button", { name: "Copy tất cả" }).first().click();
  await expect(page.getByRole("button", { name: "Đã copy" }).first()).toBeVisible();

  const downloadPromise = page.waitForEvent("download");
  await page.getByRole("button", { name: "Tải Markdown" }).click();
  const download = await downloadPromise;
  expect(download.suggestedFilename()).toMatch(/launch-kit|ao-khoac|o-kho-c/i);

  await page.getByRole("button", { name: "Đã dùng để đăng" }).click();
  await page.getByRole("button", { name: "Đã sửa trước khi dùng" }).click();
  await page.getByPlaceholder("Bạn đã sửa gì? Kit này có giúp đăng nhanh hơn không?").fill("Copied title and edited one line.");
  await page.getByRole("button", { name: "Lưu feedback" }).click();
  await expect(page.getByText("Đã lưu feedback.")).toBeVisible();

  await page.goto("/products");
  await expect(page.getByRole("heading", { name: /商品列表|商品创作工作台|ProductFlow/i }).first()).toBeVisible();
});

test("mobile seller can create and generate a LaunchKit", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await login(page);

  await createLaunchKit(page, "Combo kẹp tóc đi học mobile", {
    referenceText: "Set 5 kẹp tóc nhiều màu, nhẹ, hợp học sinh sinh viên.",
    notes: "Ưu tiên câu ngắn, dễ copy trên điện thoại.",
  });

  await expect(page.getByText("Tạo nội dung trước khi tải Markdown.")).toBeVisible();
  await expect(page.getByRole("button", { name: "Tải Markdown" })).toBeDisabled();
  await page.getByRole("button", { name: "Tạo nội dung" }).click();
  await expect(page.getByRole("heading", { name: "Proof-first practical value" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Copy tiêu đề" }).first()).toBeVisible();
  await expect(page.getByRole("button", { name: "Tải Markdown" })).toBeEnabled();
});

test("draft LaunchKit clearly blocks markdown export before generation", async ({ page }) => {
  await login(page);
  await createLaunchKit(page, "Bình nước văn phòng draft");

  await expect(page.getByText("Tạo nội dung trước khi tải Markdown.")).toBeVisible();
  await expect(page.getByRole("button", { name: "Tải Markdown" })).toBeDisabled();
  await expect(page.getByText("Chưa có tác vụ tạo nội dung.")).toBeVisible();
});

test("oversized reference input shows the backend validation detail", async ({ page }) => {
  await login(page);
  await createLaunchKit(page, "Balo laptop quá nhiều thông tin", {
    referenceText: "x".repeat(13_000),
  });

  await page.getByRole("button", { name: "Tạo nội dung" }).click();
  await expect(page.getByText(/reference input is too long/i)).toBeVisible();
  await expect(page.getByRole("button", { name: "Tạo nội dung" })).toBeEnabled();
});

test("failed generation state exposes retry and accepts a successful retry response", async ({ page }) => {
  await login(page);
  const now = new Date().toISOString();
  const failedKit = {
    id: "mock-failed-launch-kit",
    product_id: "mock-product",
    product_name: "Máy xay mini retry",
    category_key: "home_goods",
    target_platforms: ["shopee"],
    status: "failed",
    latest_task: {
      id: "task-failed",
      status: "failed",
      progress_stage: "generating_copy",
      attempt_count: 1,
      failure_category: "timeout",
      failure_detail: "Provider timeout, please retry.",
      is_retryable: true,
      is_cancelable: false,
      started_at: now,
      progress_updated_at: now,
      finished_at: now,
      created_at: now,
      updated_at: now,
    },
    quality_score_summary: null,
    created_at: now,
    updated_at: now,
    buyer_angle_key: null,
    source_references: { pasted_reference_text: "Cối nhỏ, dễ vệ sinh." },
    generated_summary: null,
    selected_angle: null,
    export_snapshot: null,
    seller_feedback: null,
    variants: [],
    exports: [],
  };
  const readyKit = {
    ...failedKit,
    status: "ready",
    latest_task: { ...failedKit.latest_task, id: "task-retry", status: "succeeded", progress_stage: "exporting_optional_snapshot", failure_detail: null, attempt_count: 2 },
    quality_score_summary: { overall: 82, warnings: [] },
    selected_angle: { label: "Proof-first practical value", why_it_might_work: "Shows practical proof.", buyer_emotion: "Tin tưởng", risk: "Thiếu ảnh thật" },
    generated_summary: { product_facts: { product_name: "Máy xay mini retry" } },
    export_snapshot: { manual_export: { platform_blocks: [{ platform: "shopee", title: "Máy xay mini retry", hook: "Nhỏ gọn cho bếp nhỏ", description: "Dễ dùng, dễ vệ sinh.", hashtags: ["#mayxaymini"] }], checklist: ["Kiểm tra ảnh thật"] } },
    variants: [{ kind: "full_kit", platform: "shopee" }],
    exports: [{ export_type: "markdown", status: "ready" }],
  };

  await page.route("**/api/launch-kits/mock-failed-launch-kit", async (route) => {
    if (route.request().method() === "GET") {
      await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(failedKit) });
      return;
    }
    await route.fallback();
  });
  await page.route("**/api/launch-kits/mock-failed-launch-kit/generate", async (route) => {
    await route.fulfill({ status: 202, contentType: "application/json", body: JSON.stringify(readyKit) });
  });

  await page.goto("/launch-kits/mock-failed-launch-kit");
  await expect(page.getByRole("heading", { name: "Máy xay mini retry" })).toBeVisible();
  await expect(page.getByText("Provider timeout, please retry.")).toBeVisible();
  await page.getByRole("button", { name: "Tạo lại sau lỗi" }).click();
  await expect(page.getByText("/ 100 sẵn sàng")).toBeVisible();
  await expect(page.getByRole("button", { name: "Copy tiêu đề" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Tải Markdown" })).toBeEnabled();
});
