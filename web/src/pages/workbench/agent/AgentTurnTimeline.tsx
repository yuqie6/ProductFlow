import { Brain, ChevronDown } from "lucide-react";
import { useState } from "react";

import { useI18n } from "../../../lib/preferences";
import type { AgentToolStep } from "../../../lib/types";
import {
  splitAgentTurnProcess,
  type AgentTurnBlock,
} from "./agentEventReducer";
import { AgentAssistantMarkdown } from "./AgentAssistantMarkdown";
import { AgentToolStepRow } from "./AgentToolStepList";

interface AgentTurnTimelineProps {
  blocks: readonly AgentTurnBlock[];
  toolSteps: readonly AgentToolStep[];
  live: boolean;
  fold: boolean;
  textSettled?: boolean;
}

export function AgentTurnTimeline({ blocks, toolSteps, live, fold, textSettled = false }: AgentTurnTimelineProps) {
  const { t } = useI18n();
  const stepsById = new Map(toolSteps.map((step) => [step.step_id, step]));
  const { process, body } = splitAgentTurnProcess(blocks);
  const shouldFold = fold && body !== null && process.length > 0;
  const toolCount = process.filter((block) => block.type === "tool").length;
  const processLabel = toolCount > 0
    ? t("agentWorkbench.process.steps", { count: toolCount })
    : t("agentWorkbench.process.thought");

  if (shouldFold && body) {
    return (
      <div className="space-y-3">
        <details
          data-agent-turn-process
          className="group/process min-w-0"
        >
          <summary
            aria-label={t("agentWorkbench.process.listLabel")}
            className="flex list-none cursor-pointer items-center gap-1.5 text-xs font-medium text-text-muted transition-colors hover:text-text-secondary [&::-webkit-details-marker]:hidden"
          >
            <ChevronDown size={13} className="shrink-0 transition-transform group-open/process:rotate-180" aria-hidden="true" />
            <span>{processLabel}</span>
          </summary>
          <div className="mt-2 space-y-3">
            {process.map((block) => (
              <TurnBlockView
                key={block.key}
                block={block}
                stepsById={stepsById}
                live={false}
                runningThinking={false}
                streamingText={false}
              />
            ))}
          </div>
        </details>
        <div data-agent-turn-body>
          <TurnBlockView
            block={body}
            stepsById={stepsById}
            live={false}
            runningThinking={false}
            streamingText={false}
          />
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-3">
      {blocks.map((block, index) => (
        <TurnBlockView
          key={block.key}
          block={block}
          stepsById={stepsById}
          live={live}
          runningThinking={live && block.type === "thinking" && index === lastThinkingIndex(blocks)}
          streamingText={live && !textSettled && block.type === "text" && index === lastTextIndex(blocks)}
        />
      ))}
    </div>
  );
}

function TurnBlockView({
  block,
  stepsById,
  live,
  runningThinking,
  streamingText,
}: {
  block: AgentTurnBlock;
  stepsById: Map<string, AgentToolStep>;
  live: boolean;
  runningThinking: boolean;
  streamingText: boolean;
}) {
  if (block.type === "thinking") {
    return (
      <div data-agent-turn-block-kind="thinking" data-agent-conversation-node="thinking">
        <AgentThinkingRow text={block.text} truncated={block.truncated} running={runningThinking} live={live} />
      </div>
    );
  }
  if (block.type === "text") {
    return (
      <div data-agent-turn-block-kind="text" data-agent-conversation-node="assistant">
        <AgentAssistantMarkdown text={block.text} streaming={streamingText} />
      </div>
    );
  }
  const step = stepsById.get(block.step_id);
  if (!step) {
    return null;
  }
  return (
    <div data-agent-turn-block-kind="tool" data-agent-conversation-node="tool">
      <AgentToolStepRow step={step} />
    </div>
  );
}

function AgentThinkingRow({
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
        {summary ? (
          <span className="min-w-0 flex-1 truncate text-text-muted">{summary}</span>
        ) : null}
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

function lastThinkingIndex(blocks: readonly AgentTurnBlock[]): number {
  for (let index = blocks.length - 1; index >= 0; index -= 1) {
    if (blocks[index].type === "thinking") {
      return index;
    }
  }
  return -1;
}

function lastTextIndex(blocks: readonly AgentTurnBlock[]): number {
  for (let index = blocks.length - 1; index >= 0; index -= 1) {
    if (blocks[index].type === "text") {
      return index;
    }
  }
  return -1;
}
