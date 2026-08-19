import { randomUUID } from "node:crypto";
import { mkdir } from "node:fs/promises";
import { join } from "node:path";
import {
  createAgentSession,
  DefaultResourceLoader,
  ModelRuntime,
  SessionManager,
  SettingsManager,
  type AgentSession,
  type AgentSessionEvent,
  type InlineExtension,
  type ToolDefinition,
} from "@earendil-works/pi-coding-agent";
import type { ImageContent, Model } from "@earendil-works/pi-ai/compat";
import {
  API_VERSION,
  CONTEXT_SCHEMA_VERSION,
  EVENT_SCHEMA_VERSION,
  MAX_DYNAMIC_CONTEXT_BYTES,
  PI_SDK_VERSION,
  ProductFlowError,
  ProductFlowContract,
  RUNTIME_NAME,
  RuntimeContext,
  RuntimeStatus,
  Scope,
  StartTurnInput,
  TurnAnswer,
  TurnArtifact,
  TurnEvent,
  TurnQuestion,
  TurnState,
  ToolStepKind,
  TurnStatus,
  TOOL_CONTRACT_VERSION,
  byteLength,
  isTerminalStatus,
  nowISO,
  safeErrorMessage,
  sameRuntimeScope,
  sha256,
  validatePageContext,
  validateScope,
} from "./contracts.js";
import { Config } from "./config.js";
import { ProductFlowClient } from "./productflow.js";
import { PRODUCTFLOW_SKILL_TOOL_NAME, SkillCatalog } from "./skills.js";
import { RuntimeError, TurnStore } from "./store.js";
import { createProductFlowTools, ToolRuntime } from "./tools.js";

const RUNTIME_VERSION = "0.1.0";
const MAX_INPUT_TEXT_BYTES = 64 << 10;

interface ProviderRequestOptions {
  reasoningSummary: string | null;
  textVerbosity: string | null;
  serviceTier: string | null;
}

export interface RuntimeLookup {
  conversationID?: string;
  taskID?: string;
}

export interface StartRequest {
  lookup: RuntimeLookup;
  input: StartTurnInput;
}

export class PiRuntimeManager {
  private readonly runs = new Map<string, RunRuntime>();
  private readonly pending: Array<{ runtime: RunRuntime; turnID: string }> = [];
  private readonly scheduled = new Set<string>();
  private running = 0;
  private closed = false;

  constructor(
    readonly config: Config,
    readonly store: TurnStore,
    readonly productFlow: ProductFlowClient,
    readonly skills: SkillCatalog,
  ) {}

  async start(request: StartRequest): Promise<TurnState> {
    this.assertOpen();
    const scope = await this.loadScope(request.lookup);
    validatePageContext(request.input.page_context);
    if (!request.input.input_text.trim() || Buffer.byteLength(request.input.input_text, "utf8") > MAX_INPUT_TEXT_BYTES) {
      throw new RuntimeError(400, "invalid_argument", "input_text is required and exceeds the limit");
    }
    const runtime = await this.runtimeFor(scope);
    const result = await this.store.createTurn(scope, request.input);
    if (result.created || result.state.status === "queued") this.enqueue(runtime, result.state.turn_id);
    return result.state;
  }

  async get(request: RuntimeLookup, turnID: string): Promise<TurnState> {
    const runtime = await this.runtimeForLookup(request);
    return this.store.getState(runtime.scope.run_id, turnID);
  }

  async cancel(request: RuntimeLookup, turnID: string): Promise<TurnState> {
    const runtime = await this.runtimeForLookup(request);
    const state = await this.store.getState(runtime.scope.run_id, turnID);
    if (isTerminalStatus(state.status)) return state;
    if (state.status === "queued" && !runtime.hasQuestionWaiter(turnID)) {
      return this.store.terminal(runtime.scope.run_id, turnID, "canceled", { output: state.output });
    }
    await this.store.updateState(runtime.scope.run_id, turnID, { status: "cancel_requested" });
    await this.store.appendEvent(runtime.scope.run_id, turnID, "turn.cancel_requested", { status: "cancel_requested" });
    runtime.cancel(turnID);
    return this.store.getState(runtime.scope.run_id, turnID);
  }

  async resume(request: RuntimeLookup, turnID: string): Promise<TurnState> {
    const runtime = await this.runtimeForLookup(request);
    const state = await this.store.getState(runtime.scope.run_id, turnID);
    if (isTerminalStatus(state.status)) return state;
    if (state.status === "queued" && runtime.hasQuestionWaiter(turnID)) {
      await runtime.resumeQuestion(turnID);
      return this.store.getState(runtime.scope.run_id, turnID);
    }
    if (state.status === "queued") {
      await this.store.appendEvent(runtime.scope.run_id, turnID, "turn.resume_requested", { status: "queued" });
      this.enqueue(runtime, turnID);
      return state;
    }
    if (state.status === "requires_input") {
      if (!runtime.hasQuestionWaiter(turnID)) {
        throw new RuntimeError(409, "not_resumable", "this question is no longer attached to a live Pi turn");
      }
      return state;
    }
    throw new RuntimeError(409, "not_resumable", "the Agent Turn is already running or has no resumable checkpoint");
  }

  async answerQuestion(request: RuntimeLookup, turnID: string, questionID: string, answer: TurnAnswer): Promise<TurnState> {
    const runtime = await this.runtimeForLookup(request);
    return runtime.answerQuestion(turnID, questionID, answer);
  }

  async events(request: RuntimeLookup, turnID: string, after: number): Promise<TurnStateAndEvents> {
    const runtime = await this.runtimeForLookup(request);
    return {
      state: await this.store.getState(runtime.scope.run_id, turnID),
      events: await this.store.events(runtime.scope.run_id, turnID, after),
    };
  }

  async *streamEvents(
    request: RuntimeLookup,
    turnID: string,
    after: number,
    signal: AbortSignal,
  ): AsyncGenerator<TurnEvent | null> {
    const runtime = await this.runtimeForLookup(request);
    const runID = runtime.scope.run_id;
    let cursor = after;
    while (!signal.aborted) {
      const events = await this.store.events(runID, turnID, cursor);
      for (const event of events) {
        cursor = Math.max(cursor, event.sequence);
        yield event;
      }
      const state = await this.store.getState(runID, turnID);
      if (isTerminalStatus(state.status)) return;
      const woke = await this.store.waitForEvents(runID, turnID, cursor, {
        signal,
        timeoutMS: this.config.heartbeatIntervalMS,
      });
      if (!woke && !signal.aborted) yield null;
    }
  }

  health(): RuntimeStatus & { active_turns: number; queued_turns: number } {
    return {
      runtime: RUNTIME_NAME,
      runtime_version: RUNTIME_VERSION,
      pi_sdk_version: PI_SDK_VERSION,
      api_version: API_VERSION,
      tool_contract_version: TOOL_CONTRACT_VERSION,
      context_schema_version: CONTEXT_SCHEMA_VERSION,
      skill_catalog_hash: this.skills.hash,
      os_tools: [],
      background_durable_tasks: false,
      active_turns: this.running,
      queued_turns: this.pending.length,
    };
  }

  async close(): Promise<void> {
    this.closed = true;
    for (const runtime of this.runs.values()) runtime.close();
    this.pending.length = 0;
  }

  private async runtimeForLookup(lookup: RuntimeLookup): Promise<RunRuntime> {
    if (!lookup.conversationID && !lookup.taskID) {
      throw new RuntimeError(400, "invalid_argument", "conversation_id or task_id is required");
    }
    return this.runtimeFor(await this.loadScope(lookup));
  }

  private async runtimeFor(scope: Scope): Promise<RunRuntime> {
    const existing = this.runs.get(scope.run_id);
    if (existing) {
      if (!sameRuntimeScope(existing.scope, scope)) {
        throw new RuntimeError(409, "conflict", "runtime scope conflicts with the ProductFlow contract");
      }
      return existing;
    }
    const runtime = new RunRuntime(this, scope);
    this.runs.set(scope.run_id, runtime);
    try {
      await this.store.ensureRun(scope);
      return runtime;
    } catch (error) {
      if (this.runs.get(scope.run_id) === runtime) this.runs.delete(scope.run_id);
      throw error;
    }
  }

  private enqueue(runtime: RunRuntime, turnID: string): void {
    const key = `${runtime.scope.run_id}:${turnID}`;
    if (this.closed || this.scheduled.has(key)) return;
    this.scheduled.add(key);
    this.pending.push({ runtime, turnID });
    void this.drain();
  }

  private async drain(): Promise<void> {
    while (!this.closed && this.running < this.config.maxConcurrentTurns && this.pending.length > 0) {
      const index = this.pending.findIndex((candidate) => candidate.runtime.canStartTurn());
      if (index < 0) return;
      const [next] = this.pending.splice(index, 1);
      next.runtime.beginTurn(next.turnID);
      this.running += 1;
      void next.runtime
        .execute(next.turnID)
        .catch(() => undefined)
        .finally(() => {
          next.runtime.endTurn(next.turnID);
          this.running -= 1;
          this.scheduled.delete(`${next.runtime.scope.run_id}:${next.turnID}`);
          void this.drain();
        });
    }
  }

  private async loadScope(lookup: RuntimeLookup): Promise<Scope> {
    const contract = lookup.taskID
      ? await this.productFlow.taskContract(lookup.taskID)
      : await this.productFlow.conversationContract(lookup.conversationID!);
    return scopeFromContract(contract, lookup);
  }

  private assertOpen(): void {
    if (this.closed) throw new RuntimeError(503, "closed", "Agent runtime is shutting down");
  }
}

export interface TurnStateAndEvents {
  state: TurnState;
  events: Awaited<ReturnType<TurnStore["events"]>>;
}

class RunRuntime implements ToolRuntime {
  private modelRuntime?: ModelRuntime;
  private model?: Model<any>;
  private session?: AgentSession;
  private abortController?: AbortController;
  private questionWaiter?: {
    turnID: string;
    questionID: string;
    resolve: (answer: TurnAnswer) => void;
    reject: (error: Error) => void;
  };
  private pendingQuestionAnswer?: TurnAnswer;
  private artifact?: TurnArtifact;
  private workflowRunRequested = false;
  private output = "";
  private attemptID = "";
  private toolCount = 0;
  private eventChain = Promise.resolve();
  private persistenceError?: Error;
  private iterationError?: Error;
  private activeTurnID?: string;
  private providerRequestOptions: ProviderRequestOptions = {
    reasoningSummary: null,
    textVerbosity: null,
    serviceTier: null,
  };

  constructor(
    private readonly manager: PiRuntimeManager,
    readonly scope: Scope,
  ) {}

  get client(): ProductFlowClient {
    return this.manager.productFlow;
  }

  loadSkill(name: string, resourcePath?: string): Promise<string> {
    return this.manager.skills.load(name, resourcePath);
  }

  get signal(): AbortSignal {
    return this.abortController?.signal ?? AbortSignal.timeout(this.manager.config.requestTimeoutMS);
  }

  canStartTurn(): boolean {
    return this.activeTurnID === undefined;
  }

  beginTurn(turnID: string): void {
    if (!this.canStartTurn()) throw new Error("ProductFlow run already has an active Pi Turn");
    this.activeTurnID = turnID;
  }

  endTurn(turnID: string): void {
    if (this.activeTurnID === turnID) this.activeTurnID = undefined;
  }

  async execute(turnID: string): Promise<void> {
    const initial = await this.manager.store.getState(this.scope.run_id, turnID);
    if (initial.status !== "queued") return;
    this.resetTurnState();
    this.currentTurn = turnID;
    this.abortController = new AbortController();
    try {
      await this.manager.store.updateState(this.scope.run_id, turnID, { status: "running", started_at: nowISO() });
      await this.manager.store.appendEvent(this.scope.run_id, turnID, "turn.started", { status: "running" });
      const runtimeContext = await this.client.runtimeContext(this.scope.conversation_id, this.scope.task_id, this.signal);
      const images = await this.loadInputImages(initial.input);
      const { session, model } = await this.createSession(turnID, runtimeContext, initial.input.page_context, images);
      this.session = session;
      this.model = model;
      const prompt = initial.input.asset_ids.length > 0
        ? `${initial.input.input_text}\n\nProductFlow selected asset IDs for this turn (inspect with the matching ProductFlow tool when needed): ${initial.input.asset_ids.join(", ")}`
        : initial.input.input_text;
      await session.prompt(prompt, {
        images: images.length > 0 && model.input.includes("image") ? images : undefined,
        source: "rpc",
      });
      await session.waitForIdle();
      await this.eventChain;
      if (this.persistenceError) throw this.persistenceError;
      if (this.iterationError) throw this.iterationError;
      const current = await this.manager.store.getState(this.scope.run_id, turnID);
      if (current.status === "cancel_requested" || this.abortController.signal.aborted) {
        await this.manager.store.terminal(this.scope.run_id, turnID, "canceled", { output: this.output });
      } else if (this.artifact) {
        const status = this.scope.scope_type === "global" ? "succeeded" : "awaiting_confirmation";
        await this.manager.store.terminal(this.scope.run_id, turnID, status, { output: this.output, artifact: this.artifact });
      } else if (
        this.scope.scope_type === "product_workflow" &&
        this.scope.task_id === null &&
        !this.workflowRunRequested
      ) {
        await this.manager.store.terminal(this.scope.run_id, turnID, "failed", {
          output: this.output,
          error: "ProductFlow required a validated workflow draft proposal, but Pi completed without one",
        });
      } else {
        await this.manager.store.terminal(this.scope.run_id, turnID, "succeeded", { output: this.output });
      }
    } catch (error) {
      await this.eventChain;
      const current = await this.manager.store.getState(this.scope.run_id, turnID).catch(() => initial);
      if (this.iterationError) {
        await this.manager.store.terminal(this.scope.run_id, turnID, "failed", {
          output: this.output,
          error: safeErrorMessage(this.iterationError),
        });
      } else if (current.status === "cancel_requested" || this.abortController.signal.aborted) {
        await this.manager.store.terminal(this.scope.run_id, turnID, "canceled", { output: this.output });
      } else {
        await this.manager.store.terminal(this.scope.run_id, turnID, "failed", {
          output: this.output,
          error: safeErrorMessage(error),
        });
      }
    } finally {
      this.questionWaiter?.reject(new RuntimeError(409, "turn_finished", "Agent Turn finished before the question was answered"));
      this.questionWaiter = undefined;
      this.pendingQuestionAnswer = undefined;
      this.iterationError = undefined;
      this.session?.dispose();
      this.session = undefined;
      this.abortController = undefined;
      this.model = undefined;
      this.currentTurn = undefined;
    }
  }

  cancel(turnID: string): void {
    if (this.questionWaiter?.turnID === turnID) {
      this.questionWaiter.reject(new RuntimeError(499, "canceled", "Agent Turn was canceled"));
      this.questionWaiter = undefined;
    }
    this.abortController?.abort();
    void this.session?.abort();
  }

  close(): void {
    this.abortController?.abort();
    this.session?.dispose();
    this.questionWaiter?.reject(new RuntimeError(503, "closed", "Agent runtime is shutting down"));
    this.questionWaiter = undefined;
  }

  hasQuestionWaiter(turnID: string): boolean {
    return this.questionWaiter?.turnID === turnID;
  }

  async resumeQuestion(turnID: string): Promise<void> {
    const waiter = this.questionWaiter;
    const answer = this.pendingQuestionAnswer;
    if (!waiter || waiter.turnID !== turnID || !answer) {
      throw new RuntimeError(409, "not_resumable", "the answered question is no longer attached to a live Pi turn");
    }
    this.pendingQuestionAnswer = undefined;
    await this.manager.store.updateState(this.scope.run_id, turnID, { status: "running", question: undefined });
    await this.manager.store.appendEvent(this.scope.run_id, turnID, "turn.resume_requested", { status: "running" });
    this.questionWaiter = undefined;
    waiter.resolve(answer);
  }

  async askUser(question: TurnQuestion): Promise<TurnAnswer> {
    const state = await this.manager.store.getState(this.scope.run_id, this.currentTurnID());
    if (this.questionWaiter) throw new Error("Pi requested more than one unanswered question");
    const answerPromise = new Promise<TurnAnswer>((resolve, reject) => {
      this.questionWaiter = { turnID: state.turn_id, questionID: question.id, resolve, reject };
    });
    await this.manager.store.updateState(this.scope.run_id, state.turn_id, { status: "requires_input", question });
    await this.manager.store.appendEvent(this.scope.run_id, state.turn_id, "turn.requires_input", {
      status: "requires_input",
      question: question as never,
    });
    await this.manager.store.appendEvent(this.scope.run_id, state.turn_id, "question.required", question as never);
    return answerPromise;
  }

  async answerQuestion(turnID: string, questionID: string, answer: TurnAnswer): Promise<TurnState> {
    const state = await this.manager.store.getState(this.scope.run_id, turnID);
    const waiter = this.questionWaiter;
    if (this.pendingQuestionAnswer && waiter?.turnID === turnID && waiter.questionID === questionID) {
      if (JSON.stringify(this.pendingQuestionAnswer) !== JSON.stringify(answer)) {
        throw new RuntimeError(409, "question_already_answered", "the question already has a different answer");
      }
      return state;
    }
    if (state.status !== "requires_input" || !state.question || state.question.id !== questionID) {
      throw new RuntimeError(409, "question_expired", "the requested question is no longer active");
    }
    if ("option" in answer) {
      const option = answer.option;
      if (option === undefined || !Number.isInteger(option) || option < 0 || option >= state.question.options.length) {
        throw new RuntimeError(400, "invalid_argument", "question option index is out of range");
      }
    } else if (!answer.text.trim() || Buffer.byteLength(answer.text, "utf8") > 4000) {
      throw new RuntimeError(400, "invalid_argument", "question text answer is empty or exceeds the limit");
    }
    if (!waiter || waiter.turnID !== turnID || waiter.questionID !== questionID) {
      throw new RuntimeError(409, "not_resumable", "the question is no longer attached to a live Pi turn");
    }
    this.pendingQuestionAnswer = answer;
    await this.manager.store.updateState(this.scope.run_id, turnID, { status: "queued", question: undefined });
    await this.manager.store.appendEvent(this.scope.run_id, turnID, "question.answered", {
      question_id: questionID,
      answer: answer as never,
    });
    return this.manager.store.getState(this.scope.run_id, turnID);
  }

  async proposeArtifact(artifact: TurnArtifact): Promise<void> {
    if (this.artifact) throw new Error("Pi proposed more than one ProductFlow artifact in one turn");
    this.artifact = artifact;
  }

  markWorkflowRunRequested(): void {
    this.workflowRunRequested = true;
  }

  idempotencyKey(toolCallID: string): string {
    return `pi:${this.scope.run_id}:${this.currentTurnID()}:${toolCallID}`.slice(0, 200);
  }

  private async createSession(
    turnID: string,
    runtimeContext: RuntimeContext,
    pageContext: StartTurnInput["page_context"],
    images: ImageContent[],
  ): Promise<{ session: AgentSession; model: Model<any> }> {
    const { runtime, model, thinkingLevel } = await this.ensureModel();
    const workspace = await this.manager.store.workspace(this.scope.run_id);
    await mkdir(join(workspace, ".pi-agent"), { recursive: true, mode: 0o700 });
    const settingsManager = SettingsManager.inMemory({
      compaction: {
        enabled: true,
        reserveTokens: Math.max(1_024, this.manager.config.modelContextWindow - this.manager.config.autoCompactTokenLimit),
        keepRecentTokens: Math.min(16_000, Math.floor(this.manager.config.autoCompactTokenLimit / 4)),
      },
      retry: { enabled: false },
      enableAnalytics: false,
      enableInstallTelemetry: false,
    });
    const staticPrompt = [
      this.scope.system_prompt,
      "ProductFlow runtime policy:",
      "All business authority and side effects remain behind ProductFlow internal tools. ProductFlow validates every scope, revision, permission, idempotency key, draft and run request.",
      "Pi has no operating-system tools. Do not invent storage paths, provider payloads, database facts, asset URLs or completed effects.",
      "Only the versioned ProductFlow tools registered by this adapter are available. A capability mentioned in an older prompt but absent from the tool list is not available; use reviewable ProductFlow proposals for changes.",
      this.scope.task_goal?.trim() ? `Authoritative ProductFlow Task goal:\n${this.scope.task_goal.trim()}` : "",
      `Runtime: ${RUNTIME_NAME}; API contract: ${API_VERSION}; context schema: ${CONTEXT_SCHEMA_VERSION}; skill catalog: ${this.manager.skills.hash}.`,
      this.manager.skills.prompt,
    ]
      .filter(Boolean)
      .join("\n\n");
    const dynamicContext = buildDynamicContext(runtimeContext, pageContext, images);
    const resourceLoader = new DefaultResourceLoader({
      cwd: workspace,
      agentDir: join(workspace, ".pi-agent"),
      settingsManager,
      noExtensions: true,
      noSkills: true,
      extensionFactories: [providerRequestExtension(this.providerRequestOptions)],
      noPromptTemplates: true,
      noThemes: true,
      noContextFiles: true,
      systemPrompt: staticPrompt,
      systemPromptOverride: (base) => `${base ?? staticPrompt}\n\n${dynamicContext}`,
    });
    await resourceLoader.reload();
    const sessionManager = SessionManager.continueRecent(workspace, this.manager.store.sessionDir(this.scope.run_id));
    const tools = createProductFlowTools(this);
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
    this.bindSessionEvents(result.session, turnID);
    return { session: result.session, model };
  }

  private bindSessionEvents(session: AgentSession, turnID: string): void {
    session.subscribe((event: AgentSessionEvent) => {
      if (event.type === "message_update" && event.assistantMessageEvent.type === "text_delta") {
        const delta = event.assistantMessageEvent.delta;
        this.output += delta;
        this.enqueue(async () => {
          await this.manager.store.appendEvent(this.scope.run_id, turnID, "text.delta", {
            delta,
            step_id: `pi_${turnID}`,
            attempt_id: this.attemptID,
          });
          await this.manager.store.updateState(this.scope.run_id, turnID, { output: this.output });
        });
        return;
      }
      if (event.type === "tool_execution_start") {
        this.toolCount += 1;
        if (this.toolCount > this.manager.config.maxIterations) {
          this.iterationError = new Error(`Pi Agent exceeded the maximum tool iteration limit of ${this.manager.config.maxIterations}`);
          this.abortController?.abort();
          void session.abort();
          return;
        }
        if (event.toolName !== "ask_user" && event.toolName !== PRODUCTFLOW_SKILL_TOOL_NAME) {
          this.enqueue(() =>
            this.manager.store.setToolStep(this.scope.run_id, turnID, {
              step_id: event.toolCallId,
              kind: toolStepKind(event.toolName),
              summary: toolStepSummary(event.toolName),
              status: "running",
            }),
          );
        }
        return;
      }
      if (
        event.type === "tool_execution_end" &&
        event.toolName !== "ask_user" &&
        event.toolName !== PRODUCTFLOW_SKILL_TOOL_NAME
      ) {
        this.enqueue(() =>
          this.manager.store.setToolStep(this.scope.run_id, turnID, {
            step_id: event.toolCallId,
            kind: toolStepKind(event.toolName),
            summary: toolStepSummary(event.toolName),
            status: event.isError ? "failed" : "succeeded",
          }),
        );
      }
    });
  }

  private enqueue(operation: () => Promise<unknown>): void {
    this.eventChain = this.eventChain
      .then(operation)
      .catch((error: unknown) => {
        this.persistenceError = error instanceof Error ? error : new Error("Agent event persistence failed");
      })
      .then(() => undefined);
  }

  private async ensureModel(): Promise<{ runtime: ModelRuntime; model: Model<any>; thinkingLevel: ReturnType<typeof thinkingLevel> }> {
    if (this.modelRuntime && this.model) {
      return { runtime: this.modelRuntime, model: this.model, thinkingLevel: thinkingLevel(this.providerReasoningEffort) };
    }
    const provider = await this.client.providerConfig(this.signal);
    if (provider.schema_version !== 1) {
      throw new ProductFlowError(502, "provider_contract_mismatch", "ProductFlow returned an unsupported provider config schema");
    }
    const providerKind = provider.provider_kind.trim();
    const apiKey = this.manager.config.providerAPIKey || provider.api_key.trim();
    const baseURL = this.manager.config.providerBaseURL || provider.base_url;
    const modelID = this.manager.config.providerModel || provider.model.trim();
    const reasoningEffort = this.manager.config.providerReasoningEffort || provider.reasoning_effort;
    if (!providerKind || !modelID) throw new ProductFlowError(502, "provider_config_invalid", "ProductFlow provider configuration is incomplete");
    if (providerKind === "mock") throw new ProductFlowError(503, "provider_unavailable", "mock Agent provider cannot run the Pi production adapter");
    if (providerKind === "openai" && !apiKey) throw new ProductFlowError(503, "provider_config_invalid", "ProductFlow provider configuration has no API key");
    const runtime = await ModelRuntime.create({
      authPath: join(await this.manager.store.workspace(this.scope.run_id), ".pi-agent", "auth.json"),
      modelsPath: join(await this.manager.store.workspace(this.scope.run_id), ".pi-agent", "models.json"),
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
            contextWindow: this.manager.config.modelContextWindow,
            maxTokens: Math.min(32_000, this.manager.config.modelContextWindow),
          },
        ],
      });
      model = runtime.getModel(providerKind, modelID);
    }
    if (!model) throw new ProductFlowError(502, "model_unavailable", `Pi cannot resolve model ${providerKind}/${modelID}`);
    this.providerReasoningEffort = reasoningEffort;
    this.providerRequestOptions = {
      reasoningSummary: this.manager.config.providerReasoningSummary || provider.reasoning_summary,
      textVerbosity: this.manager.config.providerTextVerbosity || provider.text_verbosity,
      serviceTier: this.manager.config.providerServiceTier || provider.service_tier,
    };
    this.modelRuntime = runtime;
    this.model = model;
    return { runtime, model, thinkingLevel: thinkingLevel(reasoningEffort) };
  }

  private providerReasoningEffort: string | null = null;

  private async loadInputImages(input: StartTurnInput): Promise<ImageContent[]> {
    const images: ImageContent[] = [];
    let totalBytes = 0;
    for (const assetID of input.asset_ids) {
      const content = await this.client.assetContent(
        this.scope.conversation_id,
        assetID,
        this.scope.scope_type === "global",
        this.signal,
      );
      totalBytes += content.sizeBytes;
      if (totalBytes > 20 << 20) throw new ProductFlowError(413, "image_limit", "selected assets exceed the Turn image byte limit");
      images.push({ type: "image", data: content.data, mimeType: content.mediaType });
    }
    return images;
  }

  private currentTurnID(): string {
    const current = this.currentTurn;
    if (!current) throw new Error("Pi tool executed outside an active turn");
    return current;
  }

  private currentTurn?: string;

  private resetTurnState(): void {
    this.output = "";
    this.artifact = undefined;
    this.workflowRunRequested = false;
    this.attemptID = randomUUID();
    this.toolCount = 0;
    this.persistenceError = undefined;
    this.iterationError = undefined;
    this.eventChain = Promise.resolve();
  }
}

function scopeFromContract(contract: ProductFlowContract, lookup: RuntimeLookup): Scope {
  if (contract.schema_version !== 1 || contract.tool_contract_version !== TOOL_CONTRACT_VERSION) {
    throw new RuntimeError(
      502,
      "contract_mismatch",
      `ProductFlow Agent contract mismatch: schema_version=${contract.schema_version} tool_contract_version=${contract.tool_contract_version}`,
    );
  }
  const conversationID = contract.conversation_id.trim();
  const taskID = contract.task_id?.trim() || null;
  if (lookup.conversationID && conversationID !== lookup.conversationID) {
    throw new RuntimeError(502, "scope_mismatch", "ProductFlow returned a different conversation scope");
  }
  if (lookup.taskID && taskID !== lookup.taskID) {
    throw new RuntimeError(502, "scope_mismatch", "ProductFlow returned a different task scope");
  }
  if (contract.scope_type !== "global" && contract.scope_type !== "product_workflow") {
    throw new RuntimeError(502, "contract_mismatch", `ProductFlow returned an invalid Agent scope_type ${contract.scope_type}`);
  }
  const scope: Scope = {
    schema_version: 1,
    scope_type: contract.scope_type,
    conversation_id: conversationID,
    task_id: taskID,
    task_goal: contract.task_goal,
    product_id: contract.product_id?.trim() || null,
    workflow_draft_id: contract.workflow_draft_id?.trim() || null,
    run_id: contract.harness_run_id.trim(),
    system_prompt: contract.system_prompt,
    draft_schema: contract.draft_schema,
    workflow_draft_schema: contract.workflow_draft_schema,
    current_draft_version: contract.current_draft_version,
  };
  validateScope(scope);
  return scope;
}

function buildDynamicContext(
  runtimeContext: RuntimeContext,
  pageContext: StartTurnInput["page_context"],
  images: ImageContent[],
): string {
  const snapshot = {
    schema_version: CONTEXT_SCHEMA_VERSION,
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

function providerRequestExtension(options: ProviderRequestOptions): InlineExtension {
  return {
    name: "productflow-provider-options",
    hidden: true,
    factory: (pi) => {
      pi.on("before_provider_request", (event) => {
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
        if (verbosity) {
          payload.text = { ...(isRecord(payload.text) ? payload.text : {}), verbosity };
        }
        const serviceTier = options.serviceTier?.trim();
        if (serviceTier) payload.service_tier = serviceTier;
        return payload;
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

function toolStepKind(name: string): ToolStepKind {
  if (name === "propose_workflow_draft" || name === "propose_global_draft") return "propose_draft";
  if (name === "request_workflow_run_v1") return "request_workflow_run";
  if (name === "create_product_workspace_v1") return "create_product";
  if (name.includes("legacy")) return "read_history";
  if (name.includes("inspect") && name.includes("asset")) return "inspect_image";
  if (name.includes("rename") || name.includes("folder") || name.includes("move")) return "organize_assets";
  return "inspect_context";
}

function toolStepSummary(name: string): string {
  switch (toolStepKind(name)) {
    case "inspect_image":
      return "Inspect selected image assets";
    case "propose_draft":
      return "Propose ProductFlow draft";
    case "inspect_context":
      return "Inspect ProductFlow context";
    case "read_history":
      return "Read bounded history";
    case "organize_assets":
      return "Organize ProductFlow assets";
    case "request_workflow_run":
      return "Request workflow execution for human confirmation";
    case "create_product":
      return "Create product onboarding workspace";
  }
  throw new Error(`unhandled ProductFlow tool step kind for ${name}`);
}
