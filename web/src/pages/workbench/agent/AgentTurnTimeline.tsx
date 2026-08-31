import { ChevronDown } from "lucide-react";

import { useI18n } from "../../../lib/preferences";
import type { AgentToolStep } from "../../../lib/types";
import {
  splitAgentTurnProcess,
  type AgentTurnBlock,
} from "./agentEventReducer";
import { renderConversationItem } from "./conversation/itemRenderers";

interface AgentTurnTimelineProps {
  blocks: readonly AgentTurnBlock[];
  toolSteps: readonly AgentToolStep[];
  live: boolean;
  fold: boolean;
  textSettled?: boolean;
  onCanvasFocus?: (nodeIds: string[]) => void;
}

export function AgentTurnTimeline({ blocks, toolSteps, live, fold, textSettled = false, onCanvasFocus }: AgentTurnTimelineProps) {
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
                onCanvasFocus={onCanvasFocus}
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
            onCanvasFocus={onCanvasFocus}
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
          onCanvasFocus={onCanvasFocus}
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
  onCanvasFocus,
}: {
  block: AgentTurnBlock;
  stepsById: Map<string, AgentToolStep>;
  live: boolean;
  runningThinking: boolean;
  streamingText: boolean;
  onCanvasFocus?: (nodeIds: string[]) => void;
}) {
  const step = block.type === "tool" ? stepsById.get(block.step_id) : undefined;
  const rendered = renderConversationItem({ block, step, live, runningThinking, streamingText, onCanvasFocus });
  const nodeKind = block.type === "thinking" ? "thinking" : block.type === "text" ? "assistant" : "tool";
  return (
    <div data-agent-turn-block-kind={block.type} data-agent-conversation-node={nodeKind}>
      {rendered}
    </div>
  );
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
