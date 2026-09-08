import { expect, test } from "@playwright/test";

// Standalone browser fixture exercises same-event calls that normal disabled controls serialize.
const harness = process.env.LOCALE_HARNESS_URL;
test.skip(!harness, "Set LOCALE_HARNESS_URL to the separately built localePreferences.html fixture");
for (const scenario of ["independent fields", "latest language", "retry"] as const) test(`anonymous preference loading: ${scenario}`, async ({ page }) => {
  let writes = 0;
  await page.route("**/api/auth/session", (route) => route.fulfill({ json: { authenticated: false, access_required: true } }));
  await page.route("**/api/account/preferences", (route) => { writes++; return route.fulfill({ json: {} }); });
  let release = () => {};
  const gate = new Promise<void>((resolve) => { release = resolve; });
  if (scenario !== "retry") await page.route(/\/assets\/ja-JP-[^/]+\.json$/, async (route) => { await gate; await route.continue(); });
  let attempts = 0;
  if (scenario === "retry") await page.route(/\/assets\/vi-VN-[^/]+\.json$/, (route) => ++attempts === 1 ? route.fulfill({ status: 503, body: "unavailable" }) : route.continue());
  await page.goto(harness!);
  await expect(page.locator("output")).toHaveText("zh-CN/system");
  if (scenario === "independent fields") {
    await page.getByRole("button", { name: "Language then theme", exact: true }).click();
    await expect(page.locator("output")).toHaveText("zh-CN/dark");
    release();
    await expect(page.locator("output")).toHaveText("ja-JP/dark");
  } else if (scenario === "latest language") {
    await page.getByRole("button", { name: "Two languages", exact: true }).click();
    await expect(page.locator("output")).toHaveText("vi-VN/system");
    release();
    await page.waitForLoadState("networkidle");
    await expect(page.locator("output")).toHaveText("vi-VN/system");
  } else {
    await page.getByRole("button", { name: "Vietnamese", exact: true }).click();
    await expect(page.getByRole("alert")).toHaveText("Save failed");
    await expect(page.locator("output")).toHaveText("zh-CN/system");
    await page.getByRole("button", { name: "Retry", exact: true }).click();
    await expect(page.locator("output")).toHaveText("vi-VN/system");
    expect(attempts).toBe(2);
  }
  expect(writes).toBe(0);
  const saved = await page.locator("output").textContent();
  await page.reload();
  await expect(page.locator("output")).toHaveText(saved!);
});
