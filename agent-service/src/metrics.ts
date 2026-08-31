/**
 * Agent Service 进程内 Prometheus 文本。label 只允许 bounded status/phase/tool/provider。
 */

const MAX_LABEL = 40;

const counters = {
  eventBatches: 0,
  eventBatchConflicts: 0,
  providerErrors4xx: 0,
  providerErrors5xx: 0,
};

export function recordEventBatch(): void {
  counters.eventBatches += 1;
}

export function recordEventBatchConflict(): void {
  counters.eventBatchConflicts += 1;
}

export function recordProviderHTTPStatus(status: number): void {
  if (status >= 500 && status < 600) counters.providerErrors5xx += 1;
  else if (status >= 400 && status < 500) counters.providerErrors4xx += 1;
}

export function resetMetricsForTests(): void {
  counters.eventBatches = 0;
  counters.eventBatchConflicts = 0;
  counters.providerErrors4xx = 0;
  counters.providerErrors5xx = 0;
}

export function renderMetrics(snapshot: {
  activeTurns: number;
  queuedTurns: number;
  backgroundResumable: boolean;
}): string {
  const lines = [
    "# HELP productflow_agent_service_active_turns In-process running Turns.",
    "# TYPE productflow_agent_service_active_turns gauge",
    `productflow_agent_service_active_turns ${snapshot.activeTurns}`,
    "# HELP productflow_agent_service_queued_turns In-process queued Turns.",
    "# TYPE productflow_agent_service_queued_turns gauge",
    `productflow_agent_service_queued_turns ${snapshot.queuedTurns}`,
    "# HELP productflow_agent_service_event_batches Event batches submitted to ProductFlow.",
    "# TYPE productflow_agent_service_event_batches counter",
    `productflow_agent_service_event_batches ${counters.eventBatches}`,
    "# HELP productflow_agent_service_event_batch_conflicts Event batch sequence conflicts.",
    "# TYPE productflow_agent_service_event_batch_conflicts counter",
    `productflow_agent_service_event_batch_conflicts ${counters.eventBatchConflicts}`,
    "# HELP productflow_agent_service_provider_errors Provider HTTP errors by bounded class.",
    "# TYPE productflow_agent_service_provider_errors counter",
    `productflow_agent_service_provider_errors{status="4xx"} ${counters.providerErrors4xx}`,
    `productflow_agent_service_provider_errors{status="5xx"} ${counters.providerErrors5xx}`,
    "# HELP productflow_agent_service_background_resumable Effective adapter background capability.",
    "# TYPE productflow_agent_service_background_resumable gauge",
    `productflow_agent_service_background_resumable ${snapshot.backgroundResumable ? 1 : 0}`,
  ];
  return `${lines.join("\n")}\n`;
}

export function safeMetricLabel(value: string): boolean {
  if (!value || value.length > MAX_LABEL) return false;
  return /^[a-z_]+$/u.test(value);
}
