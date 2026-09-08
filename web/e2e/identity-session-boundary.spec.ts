import { expect, test, type Page, type Route } from "@playwright/test";

import { translate } from "../src/lib/i18n";
import type { ProductListResponse, SessionState } from "../src/lib/types";

type AccountName = "anonymous" | "alice" | "bob" | "operator" | "operatorNoMerchant";

const SESSIONS: Record<AccountName, SessionState> = {
  anonymous: { authenticated: false, access_required: true, registration_available: false },
  alice: {
    authenticated: true,
    access_required: false,
    user: { id: "user-alice", email: "alice@example.com", display_name: "Alice", is_operator: false },
    merchant: { id: "merchant-alice", name: "Alice shop", status: "active" },
  },
  bob: {
    authenticated: true,
    access_required: false,
    user: { id: "user-bob", email: "bob@example.com", display_name: "Bob", is_operator: false },
    merchant: { id: "merchant-bob", name: "Bob shop", status: "active" },
  },
  operator: {
    authenticated: true,
    access_required: true,
    user: { id: "user-operator", email: "operator@example.com", display_name: "Operator", is_operator: true },
    merchant: { id: "merchant-operator", name: "Operator shop", status: "active" },
  },
  operatorNoMerchant: {
    authenticated: true,
    access_required: true,
    user: { id: "user-operator-no-merchant", email: "operator-no-merchant@example.com", display_name: "Operator", is_operator: true },
    merchant: null,
  },
};

interface IdentityMockState {
  account: AccountName;
  loginEmails: string[];
  logoutRequests: number;
  productReads: AccountName[];
  agentReads: AccountName[];
  quotaReads: string[];
}

async function fulfillJson(route: Route, status: number, payload: unknown): Promise<void> {
  await route.fulfill({ status, contentType: "application/json", body: JSON.stringify(payload) });
}

function sessionFor(state: IdentityMockState): SessionState {
  return SESSIONS[state.account];
}

function productsFor(account: AccountName): ProductListResponse {
  if (account === "anonymous") {
    return { items: [], total: 0, page: 1, page_size: 12 };
  }
  const names: Record<Exclude<AccountName, "anonymous">, string> = {
    alice: "Alice private product",
    bob: "Bob private product",
    operator: "Operator product",
    operatorNoMerchant: "Operator product",
  };
  const product = {
    id: `product-${account}`,
    name: names[account],
    category: null,
    price: null,
    cover_image_asset_id: null,
    cover_image_filename: null,
    cover_image_download_url: null,
    cover_image_preview_url: null,
    cover_image_thumbnail_url: null,
    created_at: "2026-09-08T00:00:00Z",
    updated_at: "2026-09-08T00:00:00Z",
  };
  return { items: [product], total: 1, page: 1, page_size: 12 };
}

async function installIdentityMock(page: Page): Promise<IdentityMockState> {
  const state: IdentityMockState = {
    account: "anonymous",
    loginEmails: [],
    logoutRequests: 0,
    productReads: [],
    agentReads: [],
    quotaReads: [],
  };

  await page.route("**/api/**", async (route) => {
    const request = route.request();
    const pathname = new URL(request.url()).pathname;

    if (pathname === "/api/auth/session") {
      if (request.method() === "GET") {
        await fulfillJson(route, 200, sessionFor(state));
        return;
      }
      if (request.method() === "POST") {
        const email = String((request.postDataJSON() as { email?: unknown }).email ?? "");
        state.account = email === "bob@example.com"
          ? "bob"
          : email === "operator@example.com"
            ? "operator"
            : email === "operator-no-merchant@example.com"
              ? "operatorNoMerchant"
              : "alice";
        state.loginEmails.push(email);
        await fulfillJson(route, 200, { ok: true });
        return;
      }
      if (request.method() === "DELETE") {
        state.account = "anonymous";
        state.logoutRequests += 1;
        await fulfillJson(route, 200, { ok: true });
        return;
      }
    }

    if (request.method() === "GET" && pathname === "/api/ops/merchants") {
      await fulfillJson(route, 200, { items: [], total: 0, page: 1, page_size: 20 });
      return;
    }
    if (request.method() === "GET" && pathname === "/api/v2/products") {
      state.productReads.push(state.account);
      await fulfillJson(route, 200, productsFor(state.account));
      return;
    }
    if (request.method() === "GET" && pathname === "/api/v2/agent-sessions") {
      state.agentReads.push(state.account);
      await fulfillJson(route, 200, { items: [], next_cursor: null });
      return;
    }
    if (request.method() === "GET" && pathname === "/api/v2/agent-tasks") {
      state.agentReads.push(state.account);
      await fulfillJson(route, 200, { items: [], next_cursor: null });
      return;
    }
    if (request.method() === "GET" && pathname.startsWith("/api/merchants/") && pathname.endsWith("/quota/price")) {
      state.quotaReads.push(pathname);
      await fulfillJson(route, 200, { version: 1, entries: [] });
      return;
    }
    if (request.method() === "GET" && pathname === "/api/settings/runtime") {
      await fulfillJson(route, 200, {
        image_generation_max_dimension: 2048,
        image_tool_allowed_fields: [],
        admin_access_required: true,
        deletion_enabled: false,
      });
      return;
    }
    if (request.method() === "GET" && pathname === "/api/settings") {
      await fulfillJson(route, 200, { items: [] });
      return;
    }
    if (request.method() === "GET" && pathname === "/api/settings/provider-config") {
      await fulfillJson(route, 200, { profiles: [], bindings: [] });
      return;
    }


    await fulfillJson(route, 200, { items: [], next_cursor: null });
  });

  return state;
}

async function installEventSourceRaceFixture(page: Page): Promise<void> {
  await page.addInitScript(() => {
    const sources: FakeEventSource[] = [];
    class FakeEventSource extends EventTarget {
      readonly url: string;
      readyState = 1;

      constructor(url: string) {
        super();
        this.url = url;
        sources.push(this);
      }

      close(): void {
        this.readyState = 2;
      }

      // Keep the listener after close so a dispatched event represents a response already in flight.
      removeEventListener(
        _type: string,
        _callback: EventListenerOrEventListenerObject | null,
        _options?: boolean | EventListenerOptions,
      ): void {
        void _type;
        void _callback;
        void _options;
      }

      emit(type: string, data = ""): void {
        const event = new Event(type);
        Object.defineProperty(event, "data", { value: data });
        this.dispatchEvent(event);
      }
    }

    Object.defineProperty(window, "EventSource", { configurable: true, value: FakeEventSource });
    Object.defineProperty(window, "__productFlowIdentitySources", { configurable: true, value: sources });
  });
}

async function loginAs(page: Page, email: string): Promise<void> {
  await page.locator("#auth-email").fill(email);
  await page.locator("#auth-password").fill("password123");
  await page.getByRole("button", { name: translate("zh-CN", "login.submit"), exact: true }).click();
  await expect(page).toHaveURL(email === "operator-no-merchant@example.com" ? /\/ops$/ : /\/products/);
}

test("login, logout, and account switching isolate data and fence late SSE", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.addInitScript(() => {
    localStorage.setItem("productflow.locale", "zh-CN");
    localStorage.setItem("productflow.theme", "dark");
  });
  await installEventSourceRaceFixture(page);
  const state = await installIdentityMock(page);

  await page.goto("/login");
  await loginAs(page, "alice@example.com");
  await expect(page.getByText("Alice private product", { exact: true })).toBeVisible();

  await page.locator("[data-global-agent-launcher]").click();
  await expect(page.locator("#global-agent-dock-panel")).toBeVisible();
  await expect.poll(() => page.evaluate(() => (
    (window as Window & { __productFlowIdentitySources?: unknown[] }).__productFlowIdentitySources?.length ?? 0
  ))).toBe(1);

  const oldAgentReads = state.agentReads.length;
  await page.getByRole("button", { name: translate("zh-CN", "nav.logout"), exact: true }).last().click();
  await expect(page).toHaveURL(/\/login/);
  await expect.poll(() => state.logoutRequests).toBe(1);

  await loginAs(page, "bob@example.com");
  await expect(page.getByText("Bob private product", { exact: true })).toBeVisible();
  await expect(page.getByText("Alice private product", { exact: true })).toHaveCount(0);
  expect(state.productReads).toContain("bob");

  const bobAgentReads = state.agentReads.length;
  await page.evaluate(() => {
    const source = (window as Window & {
      __productFlowIdentitySources?: Array<{ emit: (type: string, data?: string) => void }>;
    }).__productFlowIdentitySources?.[0];
    source?.emit("session.changed");
  });
  await page.waitForTimeout(250);
  expect(state.agentReads.length).toBe(bobAgentReads);
  expect(oldAgentReads).toBeLessThan(bobAgentReads);
  expect(state.loginEmails).toEqual(["alice@example.com", "bob@example.com"]);
});

test("operator settings expose the operations entry and ordinary accounts cannot enter", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.addInitScript(() => {
    localStorage.setItem("productflow.locale", "zh-CN");
    localStorage.setItem("productflow.theme", "light");
  });
  const state = await installIdentityMock(page);

  await page.goto("/login");
  await loginAs(page, "operator@example.com");
  const settingsLink = page.getByRole("link", { name: translate("zh-CN", "nav.settings"), exact: true });
  await expect(settingsLink).toBeVisible();
  await settingsLink.click();
  await expect(page).toHaveURL(/\/settings/);
  await expect(page.getByRole("link", { name: translate("zh-CN", "ops.title"), exact: true })).toBeVisible();

  await page.getByRole("button", { name: translate("zh-CN", "nav.logout"), exact: true }).last().click();
  await expect(page).toHaveURL(/\/login/);
  await loginAs(page, "alice@example.com");
  await expect(page.getByRole("link", { name: translate("zh-CN", "nav.settings"), exact: true })).toHaveCount(0);
  await page.goto("/settings");
  await expect(page).toHaveURL(/\/home/);
  expect(state.logoutRequests).toBe(1);
});

test("operator without a merchant can open settings without merchant controls or empty-id quota reads", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.addInitScript(() => {
    localStorage.setItem("productflow.locale", "zh-CN");
    localStorage.setItem("productflow.theme", "light");
  });
  const state = await installIdentityMock(page);

  await page.goto("/login");
  await loginAs(page, "operator-no-merchant@example.com");
  await page.goto("/settings");
  await expect(page).toHaveURL(/\/settings/);
  await expect(page.getByText(translate("zh-CN", "settings.title"), { exact: true }).first()).toBeVisible();
  expect(state.quotaReads).toEqual([]);
});
