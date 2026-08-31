import { Brain, ChevronDown } from "lucide-react";
import { useState, type ReactNode } from "react";

import type { AgentToolStep } from "../../../../lib/types";
import { useI18n } from "../../../../lib/preferences";
import type { AgentTurnBlock } from "../agentEventReducer";
import { AgentAssistantMarkdown } from "../AgentAssistantMarkdown";
import { AgentToolStepRow } from "../AgentToolStepList";
import { ItemCard } from "./ItemCard";

export interface ConversationItemRenderContext {
  block: AgentTurnBlock;
  step?: AgentToolStep;
  live: boolean;
  runningThinking: boolean;
  streamingText: boolean;
  onCanvasFocus?: (nodeIds: string[]) => void;
}

export type ConversationItemRenderer = (context: ConversationItemRenderContext) => ReactNode;

const renderers = new Map<string, ConversationItemRenderer>();

export function registerConversationItemRenderer(
  key: string,
  renderer: ConversationItemRenderer,
): void {
  if (!key.trim()) {
    throw new Error("Conversation item renderer key is required");
  }
  renderers.set(key, renderer);
}

export function conversationItemRenderer(key: string): ConversationItemRenderer | undefined {
  return renderers.get(key);
}

export function renderConversationItem(context: ConversationItemRenderContext): ReactNode {
  const { block, step } = context;
  if (block.type === "thinking") {
    return renderers.get("thinking")?.(context) ?? null;
  }
  if (block.type === "text") {
    return renderers.get("assistant_text")?.(context) ?? null;
  }
  if (!step) {
    return null;
  }
  const toolRenderer = step.tool_name ? renderers.get(step.tool_name) : undefined;
  const renderer = toolRenderer
    ?? renderers.get(step.kind)
    ?? renderers.get("tool_call")
    ?? renderers.get("generic_tool");
  return renderer?.(context) ?? <GenericToolCard step={step} />;
}

export function GenericToolCard({ step }: { step: AgentToolStep }): ReactNode {
  return (
    <ItemCard>
      <div data-agent-generic-tool-card data-agent-step-kind={step.kind}>
        <AgentToolStepRow step={step} />
      </div>
    </ItemCard>
  );
}

function renderGraphChange(context: ConversationItemRenderContext): ReactNode {
  return context.step ? <GraphChangeCard step={context.step} onCanvasFocus={context.onCanvasFocus} /> : null;
}

function GraphChangeCard({
  step,
  onCanvasFocus,
}: {
  step: AgentToolStep;
  onCanvasFocus?: (nodeIds: string[]) => void;
}) {
  const { t } = useI18n();
  const meta = step.meta ?? step.details;
  const nodeIds = meta?.affected_node_ids ?? [];
  const summaries = meta?.operation_summaries ?? [];
  const pending = Boolean(meta?.pending_confirmation);
  const cardKind = step.kind === "focus_canvas"
    ? "canvas-focus"
    : step.kind === "apply_graph"
      ? "graph-apply"
      : "graph-proposal";
  return (
    <ItemCard>
      <div
        data-agent-graph-proposal-card={step.kind === "propose_graph" ? "" : undefined}
        data-agent-canvas-focus-card={step.kind === "focus_canvas" ? "" : undefined}
        data-agent-graph-apply-card={step.kind === "apply_graph" ? "" : undefined}
        data-agent-graph-change-card={cardKind}
        data-proposal-id={meta?.proposal_id ?? undefined}
      >
        <AgentToolStepRow step={step} />
        {meta?.summary ? (
          <p className="mt-2 text-[12px] leading-5 text-text-secondary">{meta.summary}</p>
        ) : null}
        {summaries.length ? (
          <ul className="mt-2 list-disc space-y-0.5 pl-5 text-[12px] leading-5 text-text-secondary">
            {summaries.map((item) => (
              <li key={item}>{item}</li>
            ))}
          </ul>
        ) : null}
        {pending || (nodeIds.length > 0 && onCanvasFocus) ? (
          <div className="mt-2 flex flex-wrap items-center gap-2">
            {pending ? (
              <span className="text-[11px] text-state-warning">{t("agentWorkbench.graphProposal.pending")}</span>
            ) : null}
            {nodeIds.length > 0 && onCanvasFocus ? (
              <button
                type="button"
                data-agent-graph-proposal-focus
                className="inline-flex h-8 items-center rounded-md border border-border-l2 bg-surface-raised px-2.5 text-[11px] font-medium text-text-secondary transition-colors hover:border-accent/40 hover:bg-accent-soft hover:text-accent"
                onClick={() => onCanvasFocus(nodeIds)}
              >
                {t("agentWorkbench.graphProposal.focus")}
              </button>
            ) : null}
          </div>
        ) : null}
      </div>
    </ItemCard>
  );
}

function renderToolCard({ step }: ConversationItemRenderContext): ReactNode {
  return step ? (
    <ItemCard>
      <AgentToolStepRow step={step} />
    </ItemCard>
  ) : null;
}

function renderThinking({ block, runningThinking, live }: ConversationItemRenderContext): ReactNode {
  return block.type === "thinking" ? (
    <AgentThinkingItem
      text={block.text}
      truncated={block.truncated}
      running={runningThinking}
      live={live}
    />
  ) : null;
}

function renderAssistantText({ block, streamingText }: ConversationItemRenderContext): ReactNode {
  return block.type === "text" ? <AgentAssistantMarkdown text={block.text} streaming={streamingText} /> : null;
}

registerConversationItemRenderer("thinking", renderThinking);
registerConversationItemRenderer("assistant_text", renderAssistantText);
registerConversationItemRenderer("tool_call", renderToolCard);

for (const key of [
  "load_skill",
  "inject_context",
  "ask_question",
  "inspect_image",
  "propose_draft",
  "inspect_context",
  "read_history",
  "organize_assets",
  "request_workflow_run",
  "create_product",
  "expand_intake",
  "discard_proposal",
  "cancel_run",
]) {
  registerConversationItemRenderer(key, renderToolCard);
}

registerConversationItemRenderer("propose_graph", renderGraphChange);
registerConversationItemRenderer("apply_graph", renderGraphChange);
registerConversationItemRenderer("focus_canvas", renderGraphChange);
registerConversationItemRenderer("generic_tool", ({ step }) => (step ? <GenericToolCard step={step} /> : null));

function AgentThinkingItem({
  text,
  truncated,
  running,
  live,
}: {
  text: string;
  truncated: boolean;
  running: boolean;
  live: boolean;
}) {
  const { t } = useI18n();
  const [userOpen, setUserOpen] = useState(false);
  const open = running || userOpen;
  const summary = running ? lastNonEmptyLine(text) : firstNonEmptyLine(text);

  return (
    <details
      data-agent-thinking-row
      data-agent-thinking-running={running || undefined}
      open={open}
      onToggle={(event) => {
        if (running) return;
        setUserOpen(event.currentTarget.open);
      }}
      className="group/thinking min-w-0"
    >
      <summary
        className="flex list-none cursor-pointer items-center gap-2 rounded-lg px-1 py-1 text-xs text-text-secondary transition-colors hover:bg-surface-subtle [&::-webkit-details-marker]:hidden"
        aria-live={live && running ? "polite" : undefined}
      >
        <Brain size={14} className="shrink-0 text-text-muted" aria-hidden="true" />
        <span className="shrink-0 font-medium">{t("agentWorkbench.thinking.label")}</span>
        {summary ? <span className="min-w-0 flex-1 truncate text-text-muted">{summary}</span> : null}
        <ChevronDown size={13} className="shrink-0 text-text-muted transition-transform group-open/thinking:rotate-180" aria-hidden="true" />
      </summary>
      {text ? (
        <pre
          data-agent-thinking-text
          className="mt-1 max-h-64 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-surface-subtle px-3 py-2 text-[12px] leading-5 text-text-secondary"
        >
          {text}
        </pre>
      ) : null}
      {truncated ? (
        <div className="mt-1 text-[11px] text-text-muted">{t("agentWorkbench.toolStep.detail.truncated")}</div>
      ) : null}
    </details>
  );
}

function firstNonEmptyLine(text: string): string {
  return text.split(/\r?\n/u).find((line) => line.trim())?.trim() ?? "";
}

function lastNonEmptyLine(text: string): string {
  const lines = text.split(/\r?\n/u).filter((line) => line.trim());
  return (lines[lines.length - 1] ?? "").trim();
}
