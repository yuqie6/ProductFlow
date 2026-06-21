import { expect, test } from "@playwright/test";

test("seller can create, generate, copy, export, give feedback, and reach Advanced mode", async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText: async () => undefined },
    });
  });
  await page.goto("/");
  await expect(page).toHaveURL(/\/login$/);

  await page.getByPlaceholder("请输入管理员密钥").fill("super-secret-admin-key");
  await page.getByRole("button", { name: /登录|Log in|Đăng nhập/i }).click();
  await expect(page).toHaveURL(/\/launch-kits$/);
  await expect(page.getByRole("heading", { name: /Shopee \/ TikTok Shop LaunchKit/i })).toBeVisible();

  await page.goto("/launch-kits/new");
  await page.getByLabel("Tên sản phẩm").fill("Áo khoác chống nắng UPF50");
  await page.getByLabel("Thông tin tham khảo").fill("Vải nhẹ, nhiều màu, size M L XL, dùng khi đi xe máy.");
  await page.getByLabel("URL tham khảo").fill("https://example.com/ao-khoac");
  await page.getByLabel("Ghi chú người bán").fill("Tone rõ ràng, tránh claim y tế.");
  await page.getByRole("button", { name: "Tạo LaunchKit" }).click();

  await expect(page).toHaveURL(/\/launch-kits\/[a-f0-9-]+$/);
  await expect(page.getByRole("heading", { name: "Áo khoác chống nắng UPF50" })).toBeVisible();

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
