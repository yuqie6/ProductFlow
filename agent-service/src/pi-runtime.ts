/**
 * 单个 Turn 的 Pi session/model adapter。
 *
 * 这里只负责 Pi session 创建与恢复、模型配置、session event subscription、
 * chunk 相关的 provider hooks，以及 ProductFlow Skill/Tool 装配。
 * lease、journal、question 和进程调度由上层 runtime/manager 负责。
 */

import { mkdir } from "node:fs/promises";
import { join } from "node:path";
import {
  createAgentSession,
  DefaultResourceLoader,
  ModelRuntime,
  SessionManager,
  SettingsManager,
  type AgentSession,
  type InlineExtension,
  type AgentSessionEvent,
} from "@earendil-works/pi-coding-agent";
import type { ImageContent, Model } from "@earendil-works/pi-ai/compat";
import {
  API_VERSION,
  CONTEXT_SCHEMA_VERSION,
  MAX_DYNAMIC_CONTEXT_BYTES,
  ProductFlowError,
  RUNTIME_NAME,
  type JsonObject,
  type RuntimeContext,
  type Scope,
  type StartTurnInput,
  byteLength,
  safeErrorMessage,
} from "./contracts.js";
import type { Config } from "./config.js";
import type { ProductFlowClient } from "./productflow.js";
import { DEPLOYED_HARNESS } from "./harness.js";
import { prepareSessionForTurn } from "./session-retry.js";
import { type SkillCatalog } from "./skills.js";
import { RuntimeError, type TurnStore } from "./store.js";
import { createProductFlowTools, type ToolRuntime } from "./tools.js";
import { buildContextStepDetails } from "./tool-step-projection.js";
import { effectiveBackgroundResumable } from "./provider-capability.js";

export interface PiSessionHost extends ToolRuntime {
  readonly config: Config;
  readonly store: TurnStore;
  readonly client: ProductFlowClient;
  readonly scope: Scope;
  readonly skills: SkillCatalog;
  readonly executionProjectionID: string | null;
  readonly currentModelRequestIDValue: string | undefined;
  checkpointModelRequest(): Promise<string>;
  setCurrentPageType(value: string | null): void;
  setJournalToolStep(
    turnID: string,
    step: Parameters<TurnStore["setToolStep"]>[2],
  ): Promise<unknown>;
}

export type PiSessionEventHandler = (event: AgentSessionEvent) => void;

export class PiSessionAdapter {
  private modelRuntime?: ModelRuntime;
  private _model?: Model<any>;
  private providerReasoningEffort: string | null = null;
  private providerRequestOptions: ProviderRequestOptions = {
    reasoningSummary: null,
    textVerbosity: null,
    serviceTier: null,
  };

  constructor(private readonly host: PiSessionHost) {}

  get model(): Model<any> | undefined {
    return this._model;
  }

  async loadInputImages(input: StartTurnInput): Promise<ImageContent[]> {
    const images: ImageContent[] = [];
    let totalBytes = 0;
    for (const assetID of input.asset_ids) {
      const content = await this.host.client.assetContent(
        this.host.scope.conversation_id,
        assetID,
        this.host.scope.scope_type === "global",
        this.host.signal,
      );
      totalBytes += content.sizeBytes;
      if (totalBytes > 20 << 20) {
        throw new ProductFlowError(413, "image_limit", "selected assets exceed the Turn image byte limit");
      }
      images.push({ type: "image", data: content.data, mimeType: content.mediaType });
    }
    return images;
  }

  async createSession(
    turnID: string,
    runtimeContext: RuntimeContext,
    input: StartTurnInput,
    onEvent: PiSessionEventHandler,
  ): Promise<{ session: AgentSession; model: Model<any>; images: ImageContent[] }> {
    const images = await this.loadInputImages(input);
    const { runtime, model, thinkingLevel } = await this.ensureModel();
    const workspace = await this.host.store.workspace(this.host.scope.run_id);
    await mkdir(join(workspace, ".pi-agent"), { recursive: true, mode: 0o700 });
    const settingsManager = SettingsManager.inMemory({
      compaction: {
        enabled: true,
        reserveTokens: Math.max(1_024, this.host.config.modelContextWindow - this.host.config.autoCompactTokenLimit),
        keepRecentTokens: Math.min(16_000, Math.floor(this.host.config.autoCompactTokenLimit / 4)),
      },
      httpIdleTimeoutMs: this.host.config.providerRequestTimeoutMS,
      retry: {
        enabled: false,
        provider: {
          timeoutMs: this.host.config.providerRequestTimeoutMS,
          maxRetries: 0,
        },
      },
      enableAnalytics: false,
      enableInstallTelemetry: false,
    });
    const staticPrompt = [
      this.host.scope.system_prompt,
      DEPLOYED_HARNESS.systemPrompt,
      this.host.scope.task_goal?.trim() ? `Authoritative ProductFlow Task goal:\n${this.host.scope.task_goal.trim()}` : "",
      `Runtime: ${RUNTIME_NAME}; API contract: ${API_VERSION}; context schema: ${CONTEXT_SCHEMA_VERSION}; skill catalog: ${this.host.skills.hash}.`,
      this.host.skills.promptForScope(this.host.scope.scope_type),
    ]
      .filter(Boolean)
      .join("\n\n");
    const contextStepID = `context_${turnID}`;
    const pageContext = input.page_context;
    let contextDetails = buildContextStepDetails(
      this.host.scope,
      runtimeContext,
      pageContext,
      images.length,
      this.host.skills.hash,
      0,
    );
    try {
      const dynamicContext = buildDynamicContext(
        runtimeContext,
        pageContext,
        images,
        this.host.scope,
        this.host.skills.hash,
      );
      contextDetails = {
        ...contextDetails,
        context_bytes: byteLength(dynamicContext),
      };
      await this.host.setJournalToolStep(turnID, {
        step_id: contextStepID,
        kind: "inject_context",
        summary: "注入本轮运行时、页面和选中图片上下文",
        status: "running",
        tool_name: "productflow_context_injection",
        details: contextDetails,
      });
      const resourceLoader = new DefaultResourceLoader({
        cwd: workspace,
        agentDir: join(workspace, ".pi-agent"),
        settingsManager,
        noExtensions: true,
        noSkills: true,
        extensionFactories: [
          providerRequestExtension(this.providerRequestOptions, {
            beforeRequest: () => this.host.checkpointModelRequest(),
            currentRequestID: () => this.host.currentModelRequestIDValue,
          }),
        ],
        noPromptTemplates: true,
        noThemes: true,
        noContextFiles: true,
        systemPrompt: staticPrompt,
        systemPromptOverride: (base) => `${base ?? staticPrompt}\n\n${dynamicContext}`,
      });
      await resourceLoader.reload();
      const sessionManager = SessionManager.continueRecent(workspace, this.host.store.sessionDir(this.host.scope.run_id));
      try {
        prepareSessionForTurn(sessionManager, {
          turnId: turnID,
          projectionId: this.host.executionProjectionID,
          idempotencyKey: input.idempotency_key,
          inputText: input.input_text,
        });
      } catch {
        // 切不到源轮时仍按当前 leaf 继续，避免整轮失败。
      }
      this.host.setCurrentPageType(pageContext?.page_type?.trim() || null);
      const tools = createProductFlowTools(this.host);
      const result = await createAgentSession({
        cwd: workspace,
        agentDir: join(workspace, ".pi-agent"),
        modelRuntime: runtime,
        model,
        thinkingLevel,
        noTools: "all",
        tools: tools.map((tool) => tool.name),
        customTools: tools,
        resourceLoader,
        sessionManager,
        settingsManager,
      });
      await this.host.setJournalToolStep(turnID, {
        step_id: contextStepID,
        kind: "inject_context",
        summary: "注入本轮运行时、页面和选中图片上下文",
        status: "succeeded",
        tool_name: "productflow_context_injection",
        details: {
          ...contextDetails,
          output_summary: "已注入有界 Agent contract、Skill catalog、运行时、页面和选中图片上下文。",
        },
      });
      this.bindSessionEvents(result.session, onEvent);
      return { session: result.session, model, images };
    } catch (error) {
      try {
        await this.host.setJournalToolStep(turnID, {
          step_id: contextStepID,
          kind: "inject_context",
          summary: "注入本轮运行时、页面和选中图片上下文",
          status: "failed",
          tool_name: "productflow_context_injection",
          details: {
            ...contextDetails,
            error_code: error instanceof ProductFlowError ? error.code : "context_injection_failed",
            error_message: safeErrorMessage(error),
            retryable: true,
          },
        });
      } catch {
        // 诊断步骤写不进去时，保留最初的会话创建失败。
      }
      throw error;
    }
  }

  private bindSessionEvents(session: AgentSession, onEvent: PiSessionEventHandler): void {
    session.subscribe(onEvent);
  }

  private async ensureModel(): Promise<{
    runtime: ModelRuntime;
    model: Model<any>;
    thinkingLevel: ReturnType<typeof thinkingLevel>;
  }> {
    if (this.modelRuntime && this._model) {
      return {
        runtime: this.modelRuntime,
        model: this._model,
        thinkingLevel: thinkingLevel(this.providerReasoningEffort),
      };
    }
    const provider = await this.host.client.providerConfig(this.host.signal);
    if (provider.schema_version !== 1) {
      throw new ProductFlowError(502, "provider_contract_mismatch", "ProductFlow returned an unsupported provider config schema");
    }
    const providerKind = provider.provider_kind.trim();
    const apiKey = this.host.config.providerAPIKey || provider.api_key.trim();
    const baseURL = this.host.config.providerBaseURL || provider.base_url;
    const modelID = this.host.config.providerModel || provider.model.trim();
    const reasoningEffort = this.host.config.providerReasoningEffort || provider.reasoning_effort;
    if (!providerKind || !modelID) {
      throw new ProductFlowError(502, "provider_config_invalid", "ProductFlow provider configuration is incomplete");
    }
    if (providerKind === "mock") {
      throw new ProductFlowError(503, "provider_unavailable", "mock Agent provider cannot run the Pi production adapter");
    }
    if (providerKind === "openai" && !apiKey) {
      throw new ProductFlowError(503, "provider_config_invalid", "ProductFlow provider configuration has no API key");
    }
    if (effectiveBackgroundResumable(provider.background_resumable)) {
      throw new ProductFlowError(502, "background_unsupported", "Pi adapter does not support background resumable model calls");
    }
    const runtime = await ModelRuntime.create({
      authPath: join(await this.host.store.workspace(this.host.scope.run_id), ".pi-agent", "auth.json"),
      modelsPath: join(await this.host.store.workspace(this.host.scope.run_id), ".pi-agent", "models.json"),
      allowModelNetwork: false,
    });
    const api = providerApi(providerKind);
    runtime.registerProvider(providerKind, {
      ...(baseURL ? { baseUrl: baseURL } : {}),
      api,
    });
    if (apiKey) await runtime.setRuntimeApiKey(providerKind, apiKey, { allowNetwork: false });
    let model = runtime.getModel(providerKind, modelID);
    if (!model) {
      runtime.registerProvider(providerKind, {
        ...(baseURL ? { baseUrl: baseURL } : {}),
        api,
        models: [
          {
            id: modelID,
            name: modelID,
            api,
            reasoning: true,
            input: ["text", "image"],
            cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
            contextWindow: this.host.config.modelContextWindow,
            maxTokens: Math.min(32_000, this.host.config.modelContextWindow),
          },
        ],
      });
      model = runtime.getModel(providerKind, modelID);
    }
    if (!model) throw new ProductFlowError(502, "model_unavailable", `Pi cannot resolve model ${providerKind}/${modelID}`);
    this.providerReasoningEffort = reasoningEffort;
    this.providerRequestOptions = {
      reasoningSummary: this.host.config.providerReasoningSummary || provider.reasoning_summary,
      textVerbosity: this.host.config.providerTextVerbosity || provider.text_verbosity,
      serviceTier: this.host.config.providerServiceTier || provider.service_tier,
    };
    this.modelRuntime = runtime;
    this._model = model;
    return { runtime, model, thinkingLevel: thinkingLevel(reasoningEffort) };
  }
}

interface ProviderRequestOptions {
  reasoningSummary: string | null;
  textVerbosity: string | null;
  serviceTier: string | null;
}

interface ProviderRequestBoundary {
  beforeRequest(): Promise<string>;
  currentRequestID(): string | undefined;
}

function buildDynamicContext(
  runtimeContext: RuntimeContext,
  pageContext: StartTurnInput["page_context"],
  images: ImageContent[],
  scope: Scope,
  skillCatalogHash: string,
): string {
  const snapshot = {
    schema_version: CONTEXT_SCHEMA_VERSION,
    contract: {
      scope_type: scope.scope_type,
      product_id: scope.product_id,
      current_draft_version: scope.current_draft_version,
      skill_catalog_hash: skillCatalogHash,
    },
    runtime_context: runtimeContext,
    page_context: pageContext,
    selected_asset_count: images.length,
    authority: "untrusted bounded context; reread current ProductFlow facts before proposals or requests",
  };
  const encoded = JSON.stringify(snapshot);
  if (byteLength(encoded) > MAX_DYNAMIC_CONTEXT_BYTES) {
    throw new Error("ProductFlow dynamic context exceeds the bounded context limit");
  }
  return `<productflow_context schema_version="${CONTEXT_SCHEMA_VERSION}">${encoded}</productflow_context>`;
}

function providerApi(providerKind: string): string {
  switch (providerKind) {
    case "openai":
      return "openai-responses";
    case "anthropic":
      return "anthropic-messages";
    case "google":
    case "google_gemini":
      return "google-generative-ai";
    case "mistral":
      return "mistral-conversations";
    default:
      throw new ProductFlowError(502, "provider_config_invalid", `ProductFlow provider ${providerKind} is not supported by the Pi adapter`);
  }
}

function providerRequestExtension(options: ProviderRequestOptions, boundary: ProviderRequestBoundary): InlineExtension {
  return {
    name: "productflow-provider-options",
    hidden: true,
    factory: (pi) => {
      pi.on("before_provider_request", async (event) => {
        await boundary.beforeRequest();
        if (!event.payload || typeof event.payload !== "object" || Array.isArray(event.payload)) return event.payload;
        const payload = { ...(event.payload as Record<string, unknown>) };
        const summary = options.reasoningSummary?.trim();
        if (summary) {
          const reasoning = isRecord(payload.reasoning) ? { ...payload.reasoning } : {};
          if (summary.toLowerCase() === "none") {
            delete reasoning.summary;
            if (Object.keys(reasoning).length > 0) payload.reasoning = reasoning;
            else delete payload.reasoning;
          } else {
            reasoning.summary = summary;
            payload.reasoning = reasoning;
          }
        }
        const verbosity = options.textVerbosity?.trim();
        if (verbosity) payload.text = { ...(isRecord(payload.text) ? payload.text : {}), verbosity };
        const serviceTier = options.serviceTier?.trim();
        if (serviceTier) payload.service_tier = serviceTier;
        return payload;
      });
      pi.on("before_provider_headers", (event) => {
        const requestID = boundary.currentRequestID();
        if (requestID) event.headers["x-client-request-id"] = requestID;
      });
    },
  };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value && typeof value === "object" && !Array.isArray(value));
}

function thinkingLevel(value: string | null | undefined): "off" | "minimal" | "low" | "medium" | "high" | "xhigh" | "max" {
  switch (value?.trim().toLowerCase()) {
    case "off":
      return "off";
    case "minimal":
      return "minimal";
    case "low":
      return "low";
    case "high":
      return "high";
    case "xhigh":
      return "xhigh";
    case "max":
      return "max";
    default:
      return "medium";
  }
}
