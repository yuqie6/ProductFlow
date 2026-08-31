import type { TurnEvent } from "./contracts.js";

export const JOURNAL_BATCH_FLUSH_MS = 20;
export const JOURNAL_BATCH_MAX_EVENTS = 64;
export const JOURNAL_BATCH_MAX_BYTES = 768 << 10;

interface JournalEventBatcherOptions {
  flushMS?: number;
  maxEvents?: number;
  maxBytes?: number;
  onBackgroundError?: (error: Error) => void;
}

/**
 * 按本地 sequence 顺序提交连续事件。只有整批成功后才从队首移除，
 * 因此 ProductFlow 的事务型 batch 失败后仍可原序重试。
 */
export class JournalEventBatcher {
  private readonly pending: TurnEvent[] = [];
  private readonly flushMS: number;
  private readonly maxEvents: number;
  private readonly maxBytes: number;
  private readonly onBackgroundError?: (error: Error) => void;
  private flushTimer?: ReturnType<typeof setTimeout>;
  private activeDrain?: Promise<void>;

  constructor(
    private readonly appendBatch: (events: readonly TurnEvent[]) => Promise<void>,
    options: JournalEventBatcherOptions = {},
  ) {
    this.flushMS = options.flushMS ?? JOURNAL_BATCH_FLUSH_MS;
    this.maxEvents = options.maxEvents ?? JOURNAL_BATCH_MAX_EVENTS;
    this.maxBytes = options.maxBytes ?? JOURNAL_BATCH_MAX_BYTES;
    this.onBackgroundError = options.onBackgroundError;
  }

  get pendingCount(): number {
    return this.pending.length;
  }

  pendingSequences(): number[] {
    return this.pending.map((event) => event.sequence);
  }

  async enqueue(event: TurnEvent, barrier = false): Promise<void> {
    this.pending.push(event);
    if (barrier || this.shouldFlushNow()) {
      await this.drain();
      return;
    }
    this.scheduleFlush();
  }

  async drain(): Promise<void> {
    this.clearFlushTimer();
    while (this.pending.length > 0 || this.activeDrain) {
      if (this.activeDrain) {
        await this.activeDrain;
        continue;
      }
      const run = this.flushLoop();
      this.activeDrain = run;
      try {
        await run;
      } finally {
        if (this.activeDrain === run) this.activeDrain = undefined;
      }
    }
  }

  dispose(): void {
    this.clearFlushTimer();
  }

  private async flushLoop(): Promise<void> {
    while (this.pending.length > 0) {
      const batch = this.nextBatch();
      await this.appendBatch(batch);
      this.pending.splice(0, batch.length);
    }
  }

  private nextBatch(): TurnEvent[] {
    const batch: TurnEvent[] = [];
    let bytes = 0;
    for (const event of this.pending) {
      const eventBytes = Buffer.byteLength(JSON.stringify(event), "utf8");
      if (batch.length > 0 && (batch.length >= this.maxEvents || bytes + eventBytes > this.maxBytes)) break;
      batch.push(event);
      bytes += eventBytes;
    }
    return batch;
  }

  private shouldFlushNow(): boolean {
    if (this.pending.length >= this.maxEvents) return true;
    let bytes = 0;
    for (const event of this.pending) {
      bytes += Buffer.byteLength(JSON.stringify(event), "utf8");
      if (bytes >= this.maxBytes) return true;
    }
    return false;
  }

  private scheduleFlush(): void {
    if (this.flushTimer || this.activeDrain) return;
    this.flushTimer = setTimeout(() => {
      this.flushTimer = undefined;
      void this.drain().catch((error: unknown) => {
        this.onBackgroundError?.(asError(error));
      });
    }, this.flushMS);
    this.flushTimer.unref?.();
  }

  private clearFlushTimer(): void {
    if (!this.flushTimer) return;
    clearTimeout(this.flushTimer);
    this.flushTimer = undefined;
  }
}

export function isJournalFlushBarrier(kind: string): boolean {
  return kind === "assistant/message"
    || kind === "turn/end"
    || kind.startsWith("tool/")
    || kind.startsWith("question/")
    || kind.startsWith("approval/");
}

function asError(error: unknown): Error {
  return error instanceof Error ? error : new Error("Agent event batch persistence failed");
}
