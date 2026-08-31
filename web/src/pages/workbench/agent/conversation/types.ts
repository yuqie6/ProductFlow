import type { AgentQuestion, AgentTurn } from "../../../../lib/types";
import type { AgentTurnBlock, AgentTurnEventState } from "../agentEventReducer";

export type ConversationPublication = "immediate" | "animation-frame";

export interface PendingUserEcho {
  text: string;
  assetIds: string[];
  createdAt: string;
}

export type ConversationNode =
  | {
      type: "user";
      key: string;
      order: number;
      text: string;
      assetIds: readonly string[];
      createdAt: string;
      pending?: boolean;
    }
  | {
      type: "thinking";
      key: string;
      order: number;
      text: string;
      truncated: boolean;
      running: boolean;
    }
  | {
      type: "assistant";
      key: string;
      order: number;
      text: string;
      streaming: boolean;
    }
  | {
      type: "tool";
      key: string;
      order: number;
      stepId: string;
    }
  | {
      type: "question";
      key: string;
      order: number;
      question: AgentQuestion;
    }
  | {
      type: "proposal";
      key: string;
      order: number;
      turnId: string;
    }
  | {
      type: "turn-tail";
      key: string;
      order: number;
      turnId: string;
    }
  | {
      type: "turn-error";
      key: string;
      order: number;
      message: string;
    };

export function echoMatchesTurn(echo: PendingUserEcho, turn: Pick<AgentTurn, "input_text" | "input_asset_ids">): boolean {
  return (
    turn.input_text === echo.text &&
    turn.input_asset_ids.length === echo.assetIds.length &&
    turn.input_asset_ids.every((id, index) => id === echo.assetIds[index])
  );
}

export function isStreamPublicationKind(kind: string): boolean {
  return kind === "item.delta";
}

export function assembleTurnNodes(input: {
  turn: AgentTurn;
  eventState: AgentTurnEventState | null;
  blocks: readonly AgentTurnBlock[];
  live: boolean;
}): ConversationNode[] {
  const { turn, eventState, blocks, live } = input;
  const nodes: ConversationNode[] = [];
  let order = 0;
  const streaming = live && !eventState?.text_settled && !eventState?.terminal_kind;
  for (const [index, block] of blocks.entries()) {
    if (block.type === "thinking") {
      nodes.push({
        type: "thinking",
        key: block.key,
        order: order++,
        text: block.text,
        truncated: block.truncated,
        running: streaming && index === lastIndexOf(blocks, "thinking"),
      });
      continue;
    }
    if (block.type === "text") {
      nodes.push({
        type: "assistant",
        key: block.key,
        order: order++,
        text: block.text,
        streaming: streaming && index === lastIndexOf(blocks, "text"),
      });
      continue;
    }
    nodes.push({ type: "tool", key: block.key, order: order++, stepId: block.step_id });
  }
  const question = eventState?.turn_key === turn.id && eventState.question
    ? eventState.question
    : turn.status === "requires_input"
      ? turn.question
      : null;
  if (question) {
    nodes.push({ type: "question", key: `question:${question.id}`, order: order++, question });
  }
  if (turn.error_text && (turn.status === "failed" || turn.status === "unknown")) {
    nodes.push({ type: "turn-error", key: `error:${turn.id}`, order: order++, message: turn.error_text });
  }
  if (turn.status === "awaiting_confirmation") {
    nodes.push({ type: "proposal", key: `proposal:${turn.id}`, order: order++, turnId: turn.id });
  }
  nodes.push({ type: "turn-tail", key: `tail:${turn.id}`, order: order, turnId: turn.id });
  return nodes;
}

function lastIndexOf(blocks: readonly AgentTurnBlock[], type: "thinking" | "text"): number {
  for (let index = blocks.length - 1; index >= 0; index -= 1) {
    if (blocks[index].type === type) return index;
  }
  return -1;
}
