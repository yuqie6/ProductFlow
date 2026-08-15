import { describe, expect, it } from "vitest";

import {
  agentBindingPayloadFromDraft,
  configValuesFromChangedDrafts,
  draftsFromConfig,
  imageBindingPayloadFromDraft,
  itemsForSection,
  promptBindingPayloadFromDraft,
  providerDisableBlocked,
  providerDrawerCreateState,
  providerDrawerEditState,
  providerFormFromProfile,
  providerProfileCreatePayload,
  providerProfileUpdatePayload,
  providerUsageFromBindings,
  providerUsageLabelKeys,
  settingsExportFilename,
  settingsImportSummaryCounts,
  settingsSectionFromSearchParam,
  settingsSectionIds,
  shouldShowSettingsMigrationPanel,
} from "./SettingsPage";
import { translate } from "../lib/i18n";
import type {
  ConfigItem,
  ConfigResponse,
  ProviderBinding,
  ProviderCapability,
  ProviderProfile,
  SettingsImportPreviewResponse,
} from "../lib/types";

function configItem(overrides: Partial<ConfigItem> & Pick<ConfigItem, "key" | "value">): ConfigItem {
  return {
    key: overrides.key,
    label: overrides.label ?? overrides.key,
    category: overrides.category ?? "测试",
    input_type: overrides.input_type ?? "text",
    description: overrides.description ?? "",
    value: overrides.value,
    source: overrides.source ?? "env_default",
    secret: overrides.secret ?? false,
    has_value: overrides.has_value ?? false,
    options: overrides.options ?? [],
    minimum: overrides.minimum ?? null,
    maximum: overrides.maximum ?? null,
    updated_at: overrides.updated_at ?? null,
  };
}

function configResponse(items: ConfigItem[]): ConfigResponse {
  return { items };
}

function providerProfile(overrides: Partial<ProviderProfile> = {}): ProviderProfile {
  return {
    id: overrides.id ?? "profile-1",
    name: overrides.name ?? "OpenRouter",
    provider_type: overrides.provider_type ?? "openai_compatible",
    base_url: "base_url" in overrides ? (overrides.base_url ?? null) : "https://openrouter.ai/api/v1",
    capabilities: overrides.capabilities ?? ["text_responses", "image_images"],
    default_models: overrides.default_models ?? {},
    config: overrides.config ?? {},
    enabled: overrides.enabled ?? true,
    archived_at: overrides.archived_at ?? null,
    has_api_key: overrides.has_api_key ?? true,
    created_at: overrides.created_at ?? "2026-05-13T00:00:00Z",
    updated_at: overrides.updated_at ?? "2026-05-13T00:00:00Z",
  };
}

function providerBinding(overrides: Partial<ProviderBinding> & Pick<ProviderBinding, "purpose">): ProviderBinding {
  return {
    id: overrides.id ?? `${overrides.purpose}-binding`,
    purpose: overrides.purpose,
    provider_kind: overrides.provider_kind ?? "openai",
    provider_profile_id: overrides.provider_profile_id ?? "profile-1",
    model_settings: overrides.model_settings ?? {},
    config: overrides.config ?? {},
    created_at: overrides.created_at ?? "2026-05-13T00:00:00Z",
    updated_at: overrides.updated_at ?? "2026-05-13T00:00:00Z",
  };
}

describe("SettingsPage draft helpers", () => {
  it("only submits changed non-secret values instead of rewriting the whole config page", () => {
    const items = [
      configItem({ key: "admin_access_required", input_type: "boolean", value: true }),
      configItem({ key: "image_main_image_size", value: "1024x1024" }),
      configItem({ key: "image_tool_allowed_fields", input_type: "multi_select", value: ["model", "quality"] }),
    ];
    const state = draftsFromConfig(configResponse(items));

    const values = configValuesFromChangedDrafts(
      items,
      {
        ...state.drafts,
        image_main_image_size: "1536x1024",
      },
      state.snapshots,
      {},
    );

    expect(values).toEqual({ image_main_image_size: "1536x1024" });
  });

  it("keeps untouched secrets out but submits touched secrets", () => {
    const items = [
      configItem({ key: "local_secret", input_type: "password", secret: true, has_value: true, value: "" }),
    ];
    const state = draftsFromConfig(configResponse(items));

    expect(configValuesFromChangedDrafts(items, state.drafts, state.snapshots, {})).toEqual({});
    expect(
      configValuesFromChangedDrafts(
        items,
        { ...state.drafts, local_secret: "sk-new" },
        state.snapshots,
        { local_secret: true },
      ),
    ).toEqual({ local_secret: "sk-new" });
  });

  it("detects multi-select changes by ordered option values", () => {
    const items = [configItem({ key: "image_tool_allowed_fields", input_type: "multi_select", value: ["model"] })];
    const state = draftsFromConfig(configResponse(items));

    const unchanged = configValuesFromChangedDrafts(items, state.drafts, state.snapshots, {});
    const changed = configValuesFromChangedDrafts(
      items,
      { image_tool_allowed_fields: ["model", "quality"] },
      state.snapshots,
      {},
    );

    expect(unchanged).toEqual({});
    expect(changed).toEqual({ image_tool_allowed_fields: ["model", "quality"] });
  });
});

describe("SettingsPage provider profile helpers", () => {
  it("opens the drawer in create mode with a clean provider form", () => {
    expect(providerDrawerCreateState()).toEqual({
      open: true,
      editingProfileId: null,
      form: {
        name: "",
        provider_type: "openai_compatible",
        base_url: "",
        api_key: "",
        capabilities: ["text_responses", "image_images"],
        enabled: true,
      },
    });
  });

  it("opens the drawer in edit mode without echoing the existing API key", () => {
    const profile = providerProfile({
      id: "profile-edit",
      name: "Custom",
      base_url: null,
      capabilities: ["text_responses", "image_responses"],
      enabled: false,
      has_api_key: true,
    });

    expect(providerDrawerEditState(profile)).toEqual({
      open: true,
      editingProfileId: "profile-edit",
      form: {
        name: "Custom",
        provider_type: "openai_compatible",
        base_url: "",
        api_key: "",
        capabilities: ["text_responses", "image_responses"],
        enabled: false,
      },
    });
    expect(providerFormFromProfile(profile).api_key).toBe("");
  });

  it("opens the drawer for a Google Gemini profile with native provider metadata", () => {
    const profile = providerProfile({
      id: "profile-gemini",
      name: "Gemini",
      provider_type: "google_gemini",
      base_url: null,
      capabilities: ["image_google_gemini"],
    });

    expect(providerDrawerEditState(profile)).toEqual({
      open: true,
      editingProfileId: "profile-gemini",
      form: {
        name: "Gemini",
        provider_type: "google_gemini",
        base_url: "",
        api_key: "",
        capabilities: ["image_google_gemini"],
        enabled: true,
      },
    });
  });

  it("builds create and edit payloads while preserving blank-key edit semantics", () => {
    const form = {
      name: "  OpenRouter  ",
      provider_type: "openai_compatible" as const,
      base_url: "  https://openrouter.ai/api/v1  ",
      api_key: "",
      capabilities: ["text_responses", "image_images"] as ProviderCapability[],
      enabled: true,
    };

    expect(providerProfileCreatePayload(form)).toEqual({
      name: "OpenRouter",
      provider_type: "openai_compatible",
      base_url: "https://openrouter.ai/api/v1",
      api_key: null,
      capabilities: ["text_responses", "image_images"],
      enabled: true,
    });
    expect(providerProfileUpdatePayload(form)).toEqual({
      name: "OpenRouter",
      provider_type: "openai_compatible",
      base_url: "https://openrouter.ai/api/v1",
      api_key: "",
      capabilities: ["text_responses", "image_images"],
      enabled: true,
    });
  });

  it("builds Google Gemini provider payloads without custom base URL", () => {
    const form = {
      name: "  Gemini  ",
      provider_type: "google_gemini" as const,
      base_url: "https://should-not-submit.example",
      api_key: "  google-key  ",
      capabilities: ["image_google_gemini"] as ProviderCapability[],
      enabled: true,
    };

    expect(providerProfileCreatePayload(form)).toEqual({
      name: "Gemini",
      provider_type: "google_gemini",
      base_url: null,
      api_key: "google-key",
      capabilities: ["image_google_gemini"],
      enabled: true,
    });
    expect(providerProfileUpdatePayload(form)).toEqual({
      name: "Gemini",
      provider_type: "google_gemini",
      base_url: null,
      api_key: "  google-key  ",
      capabilities: ["image_google_gemini"],
      enabled: true,
    });
  });

  it("derives card usage labels from prompt, Agent, image, and legacy-only text bindings", () => {
    const usage = providerUsageFromBindings(
      [
        providerBinding({ purpose: "text", provider_profile_id: "profile-1" }),
        providerBinding({ purpose: "prompt", provider_profile_id: "profile-1" }),
        providerBinding({ purpose: "agent", provider_profile_id: "profile-1" }),
        providerBinding({ purpose: "image", provider_profile_id: "profile-1", provider_kind: "openai_images" }),
        providerBinding({ purpose: "image", provider_profile_id: "other", provider_kind: "openai_images" }),
      ],
      "profile-1",
    );

    expect(usage).toEqual({ prompt: true, legacyText: false, agent: true, image: true });
    expect(providerUsageLabelKeys(usage)).toEqual([
      "settings.provider.usagePrompt",
      "settings.provider.usageAgent",
      "settings.provider.usageImage",
    ]);

    const legacyUsage = providerUsageFromBindings(
      [providerBinding({ purpose: "text", provider_profile_id: "legacy-profile" })],
      "legacy-profile",
    );
    expect(legacyUsage).toEqual({ prompt: false, legacyText: true, agent: false, image: false });
    expect(providerUsageLabelKeys(legacyUsage)).toEqual(["settings.provider.usageLegacyText"]);
  });

  it("builds Google Gemini image binding payloads without OpenAI-specific config", () => {
    expect(
      imageBindingPayloadFromDraft({
        provider_kind: "google_gemini_image",
        provider_profile_id: "profile-gemini",
        model: " gemini-2.5-flash-image ",
        images_quality: "high",
        images_style: "vivid",
        responses_background_enabled: true,
        gemini_api_version: "v1beta",
        gemini_output_mime_type: " image/png ",
      }),
    ).toEqual({
      provider_kind: "google_gemini_image",
      provider_profile_id: "profile-gemini",
      model_settings: { model: "gemini-2.5-flash-image" },
      config: { gemini_api_version: "v1beta", gemini_output_mime_type: "image/png" },
    });
  });

  it("builds prompt binding payloads with one prompt model", () => {
    expect(
      promptBindingPayloadFromDraft({
        provider_kind: "openai",
        provider_profile_id: "profile-1",
        model: " gpt-5.4 ",
      }),
    ).toEqual({
      provider_kind: "openai",
      provider_profile_id: "profile-1",
      model_settings: { model: "gpt-5.4" },
      config: {},
    });
  });

  it("builds workflow Agent binding payloads with Responses runtime options", () => {
    expect(
      agentBindingPayloadFromDraft({
        provider_kind: "openai",
        provider_profile_id: "profile-1",
        model: " gpt-5.5 ",
        reasoning_effort: " high ",
        reasoning_summary: " concise ",
        text_verbosity: " ",
        service_tier: " priority ",
      }),
    ).toEqual({
      provider_kind: "openai",
      provider_profile_id: "profile-1",
      model_settings: { model: "gpt-5.5" },
      config: {
        reasoning_effort: "high",
        reasoning_summary: "concise",
        service_tier: "priority",
      },
    });
  });

  it("blocks disabling an enabled provider that is currently used by a binding", () => {
    expect(
      providerDisableBlocked(providerProfile({ enabled: true }), {
        prompt: true,
        legacyText: false,
        agent: false,
        image: false,
      }),
    ).toBe(true);
    expect(
      providerDisableBlocked(providerProfile({ enabled: true }), {
        prompt: false,
        legacyText: true,
        agent: false,
        image: false,
      }),
    ).toBe(true);
    expect(
      providerDisableBlocked(providerProfile({ enabled: true }), {
        prompt: false,
        legacyText: false,
        agent: true,
        image: false,
      }),
    ).toBe(true);
    expect(
      providerDisableBlocked(providerProfile({ enabled: true }), {
        prompt: false,
        legacyText: false,
        agent: false,
        image: false,
      }),
    ).toBe(false);
    expect(
      providerDisableBlocked(providerProfile({ enabled: false }), {
        prompt: true,
        legacyText: true,
        agent: true,
        image: true,
      }),
    ).toBe(false);
  });

  it("localizes the provider delete confirmation dialog copy", () => {
    expect(translate("zh-CN", "settings.provider.deleteConfirmTitle")).toBe("删除供应商");
    expect(translate("zh-CN", "settings.provider.deleteConfirm", { name: "OpenRouter" })).toBe(
      "确定删除「OpenRouter」吗？",
    );
    expect(translate("zh-CN", "settings.provider.deleteConfirmLabel")).toBe("删除");
    expect(translate("en-US", "settings.provider.deleteConfirmTitle")).toBe("Delete provider");
    expect(translate("en-US", "settings.provider.deleteConfirm", { name: "OpenRouter" })).toBe(
      'Delete "OpenRouter"?',
    );
    expect(translate("en-US", "settings.provider.deleteConfirmLabel")).toBe("Delete");
  });

  it("localizes Google Gemini provider labels", () => {
    expect(translate("zh-CN", "settings.provider.capability.imageGoogleGemini")).toBe("Google Gemini 图片");
    expect(translate("zh-CN", "settings.provider.type.googleGemini")).toBe("Google Gemini");
    expect(translate("zh-CN", "settings.provider.interface.googleGeminiImage")).toBe(
      "Google Gemini Image (未实测)",
    );
    expect(translate("en-US", "settings.provider.capability.imageGoogleGemini")).toBe("Google Gemini image");
    expect(translate("en-US", "settings.provider.type.googleGemini")).toBe("Google Gemini");
    expect(translate("en-US", "settings.provider.interface.googleGeminiImage")).toBe(
      "Google Gemini Image (untested)",
    );
  });
});

describe("SettingsPage import/export helpers", () => {
  it("keeps import and export controls on a dedicated settings section", () => {
    const sectionIds = settingsSectionIds();

    expect(sectionIds).toContain("migration");
    expect(sectionIds).toContain("prompt");
    expect(sectionIds).toContain("agent");
    expect(shouldShowSettingsMigrationPanel("migration")).toBe(true);
    for (const sectionId of sectionIds.filter((sectionId) => sectionId !== "migration")) {
      expect(shouldShowSettingsMigrationPanel(sectionId)).toBe(false);
    }
  });

  it("resolves deep-linked settings sections and rejects unknown values", () => {
    expect(settingsSectionFromSearchParam("prompt")).toBe("prompt");
    expect(settingsSectionFromSearchParam("text")).toBe("prompt");
    expect(settingsSectionFromSearchParam("agent")).toBe("agent");
    expect(settingsSectionFromSearchParam("migration")).toBe("migration");
    expect(settingsSectionFromSearchParam("unknown")).toBe("providers");
    expect(settingsSectionFromSearchParam(null)).toBe("providers");
  });

  it("hides retired copy system prompts from the normal image request template section", () => {
    const config = configResponse([
      configItem({ key: "prompt_brief_system", value: "legacy brief", category: "提示词" }),
      configItem({ key: "prompt_copy_system", value: "legacy copy", category: "提示词" }),
      configItem({ key: "prompt_image_system", value: "image request", category: "提示词" }),
      configItem({ key: "upload_max_files", value: 6, category: "海报与上传" }),
    ]);

    expect(itemsForSection(config, "prompts").map((item) => item.key)).toEqual(["prompt_image_system"]);
  });

  it("builds a stable JSON export filename from the export timestamp", () => {
    expect(settingsExportFilename("2026-05-14T01:02:03Z")).toBe("productflow-settings-2026-05-14-010203.json");
    expect(settingsExportFilename(null)).toBe("productflow-settings.json");
  });

  it("normalizes import preview summary counts for confirmation copy", () => {
    const preview: SettingsImportPreviewResponse = {
      schema_version: 1,
      runtime_config_count: 12,
      provider_profile_count: 2,
      provider_binding_count: 2,
      provider_profile_names: ["主供应商", "备用供应商"],
      provider_binding_purposes: ["image", "text"],
      includes_api_keys: true,
      provider_profiles_with_api_key_count: 1,
    };

    expect(settingsImportSummaryCounts(preview)).toEqual({
      runtimeConfigCount: 12,
      providerProfileCount: 2,
      providerBindingCount: 2,
      providerProfilesWithApiKeyCount: 1,
    });
  });
});
