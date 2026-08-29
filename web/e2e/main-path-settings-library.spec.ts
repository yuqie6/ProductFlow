import { expect, test } from "@playwright/test";

import {
  assertLiveBrowserGraphEnabled,
  lockLocale,
  loginAsAdmin,
  requiredEnv,
} from "./liveGraph";

const PRESETS = [
  { name: "1440", width: 1440, height: 900 },
  { name: "390", width: 390, height: 844 },
] as const;

for (const preset of PRESETS) {
  test.describe(`main path settings and media library ${preset.name}`, () => {
    test.use({ viewport: { width: preset.width, height: preset.height } });

    test("unlocks settings and opens the global media library", async ({ page }) => {
      assertLiveBrowserGraphEnabled();
      await lockLocale(page);
      await loginAsAdmin(page, requiredEnv("ADMIN_ACCESS_KEY"));

      await page.goto("/settings");
      await expect(page.getByRole("heading", { name: "需要二次令牌才能查看系统配置" })).toBeVisible();
      await page.getByPlaceholder("输入 SETTINGS_ACCESS_TOKEN").fill(requiredEnv("SETTINGS_ACCESS_TOKEN"));
      await page.getByRole("button", { name: "解锁配置" }).click();
      await expect(page.getByText("系统配置已解锁")).toBeVisible();
      await expect(page.getByRole("heading", { name: "供应商档案" })).toBeVisible();
      await expect(page.getByText("统一管理 API Key、Base URL 和接口能力。")).toBeVisible();

      await page.goto("/media-library");
      await expect(page.getByRole("heading", { name: "全局图库" })).toBeVisible();
      await expect(page.getByRole("button", { name: "上传" }).first()).toBeVisible();
    });
  });
}
