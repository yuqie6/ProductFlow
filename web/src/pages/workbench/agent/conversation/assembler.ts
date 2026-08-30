import type { AgentTurnEvent } from "../../../../lib/types";
import {
  agentEventReducer,
  createAgentTurnEventState,
  type AgentTurnEventState,
} from "../agentEventReducer";
import { isStreamPublicationKind } from "./types";

export interface FrameScheduler {
  schedule(callback: () => void): number;
  cancel(id: number): void;
}

const browserScheduler: FrameScheduler = {
  schedule: (callback) => requestAnimationFrame(callback),
  cancel: (id) => cancelAnimationFrame(id),
};

/**
 * 把 runtime 事件折进 Turn 投影。token/thinking 按 animation-frame 发布，结构事件立即发布。
 */
export class ConversationAssembler {
  private folded: AgentTurnEventState;
  private published: AgentTurnEventState;
  private frame: number | null = null;
  private readonly listeners = new Set<() => void>();

  constructor(
    turnKey: string,
    private readonly scheduler: FrameScheduler = browserScheduler,
  ) {
    this.folded = createAgentTurnEventState(turnKey);
    this.published = this.folded;
  }

  getSnapshot = (): AgentTurnEventState => this.published;

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };

  reset(turnKey: string): void {
    this.cancelFrame();
    this.folded = createAgentTurnEventState(turnKey);
    this.published = this.folded;
    this.emit();
  }

  apply(event: AgentTurnEvent): void {
    this.folded = agentEventReducer(this.folded, { type: "event", event });
    if (isStreamPublicationKind(event.kind)) {
      this.scheduleFrame();
      return;
    }
    this.cancelFrame();
    this.published = this.folded;
    this.emit();
  }

  dispose(): void {
    this.cancelFrame();
    this.listeners.clear();
  }

  private scheduleFrame(): void {
    if (this.frame !== null) return;
    this.frame = this.scheduler.schedule(() => {
      this.frame = null;
      this.published = this.folded;
      this.emit();
    });
  }

  private cancelFrame(): void {
    if (this.frame === null) return;
    this.scheduler.cancel(this.frame);
    this.frame = null;
  }

  private emit(): void {
    for (const listener of this.listeners) listener();
  }
}
