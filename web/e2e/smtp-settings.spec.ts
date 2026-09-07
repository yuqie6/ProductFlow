import { expect, test } from "@playwright/test";
import { LOCALES, translate } from "../src/lib/i18n";
import type { ConfigItem } from "../src/lib/types";

for (const width of [390, 1440]) {
  for (const locale of LOCALES) {
    test(`SMTP settings ${width} ${locale}`, async ({ page }, testInfo) => {
      await page.setViewportSize({ width, height: width === 390 ? 844 : 960 });
      await page.addInitScript(({ locale, theme }) => {
        localStorage.setItem("productflow.locale", locale);
        localStorage.setItem("productflow.theme", theme);
      }, { locale, theme: width === 390 ? "light" : "dark" });
      let config: ConfigItem[] = ["host", "port", "security", "username", "password", "from_address", "from_name"].map((field) => ({
        key: `smtp_${field}`, label: field, description: "", category: "安全与运维",
        input_type: field === "port" ? "number" : field === "security" ? "select" : "text",
        value: field === "port" ? 587 : field === "security" ? "starttls" : "",
        secret: field === "password", has_value: field === "password", source: "database",
        options: field === "security" ? [{ value: "starttls", label: "STARTTLS" }, { value: "tls", label: "TLS" }] : [],
        minimum: null, maximum: null, updated_at: null,
      }));
      const patches: Record<string, unknown>[] = [];
      await page.route("**/api/**", (route) => route.fulfill({ json: { items: [] } }));
      await page.route("**/api/auth/session", (route) => route.fulfill({ json: {
        authenticated: true, access_required: true,
        user: { id: "operator", email: "operator@example.com", display_name: "Operator", is_operator: true },
        memberships: [{ merchant_id: "merchant", merchant_name: "Shop", role: "owner", status: "active", merchant_status: "active" }],
      } }));
      await page.route("**/api/settings/provider-config", (route) => route.fulfill({ json: { profiles: [], bindings: [] } }));
      await page.route("**/api/settings", (route) => {
        if (route.request().method() === "PATCH") {
          const values = route.request().postDataJSON().values as Record<string, unknown>;
          patches.push(values);
          config = config.map((item) => ({ ...item, value: item.secret ? "" : values[item.key] as ConfigItem["value"] ?? item.value }));
        }
        return route.fulfill({ json: { items: config } });
      });
      const retiredSupportRequests: string[] = [];
      page.on("request", (request) => {
        if (request.url().includes("/api/ops/support-")) retiredSupportRequests.push(request.url());
      });
      const errors: string[] = [];
      page.on("pageerror", (error) => errors.push(error.message));
      await page.goto("/settings?section=mail");
      await expect(page.locator("#smtp_host")).toBeVisible();
      await expect(page.locator("#smtp_password")).toHaveAttribute("type", "password");
      await expect(page.locator("#smtp_password")).toHaveValue("");
      await page.locator("#smtp_host").fill("mail.example.com");
      await page.locator("#smtp_username").fill("noreply@example.com");
      await page.locator("#smtp_from_address").fill("noreply@example.com");
      await page.locator("#smtp_from_name").fill("ProductFlow");
      await page.getByRole("button", { name: translate(locale, "settings.save"), exact: true }).click();
      await expect.poll(() => patches.length).toBe(1);
      expect(patches[0]).not.toHaveProperty("smtp_password");
      await page.locator("#smtp_password").fill("new-test-password");
      await page.getByRole("button", { name: translate(locale, "settings.save"), exact: true }).click();
      await expect.poll(() => patches.length).toBe(2);
      expect(patches[1]).toEqual({ smtp_password: "new-test-password" });
      await expect(page.locator("#smtp_password")).toHaveValue("");
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      await page.screenshot({ path: testInfo.outputPath("smtp-settings.png"), fullPage: true });
      expect(retiredSupportRequests).toEqual([]);
      expect(errors).toEqual([]);
    });
  }
}
