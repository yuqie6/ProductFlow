import { useState } from "react";
import { FileText, Loader2, RotateCcw, Layers3 } from "lucide-react";

import { PromptPreviewDialog, type PromptPreview } from "../../components/PromptPreviewDialog";
import { formatDateTime } from "../../lib/format";
import { useI18n } from "../../lib/preferences";
import type { ProductWorkflow, WorkflowNode, WorkflowRun, WorkflowRunStatus } from "../../lib/types";
import { workflowNodeDisplayLabel, workflowNodeDisplayTitle } from "./nodeDisplay";
import {
  outputText,
  statusClass,
  workflowNodeRunDurationText,
  workflowNodeRunProviderSummary,
  workflowNodeRunStatusLabel,
  workflowRunQueueText,
} from "./utils";

const RUN_STATUS_CLASS_NAMES: Record<WorkflowRunStatus, string> = {
  running: "border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-400/35 dark:bg-blue-500/12 dark:text-blue-200",
  succeeded:
    "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-400/35 dark:bg-emerald-500/12 dark:text-emerald-200",
  failed: "border-red-200 bg-red-50 text-red-700 dark:border-red-400/35 dark:bg-red-500/12 dark:text-red-200",
  cancelled: "border-zinc-200 bg-zinc-50 text-zinc-600 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-300",
};

const RUN_STATUS_DOT_CLASS_NAMES: Record<WorkflowRunStatus, string> = {
  running: "bg-blue-500 shadow-blue-500/30",
  succeeded: "bg-emerald-500 shadow-emerald-500/30",
  failed: "bg-red-500 shadow-red-500/30",
  cancelled: "bg-zinc-400 shadow-zinc-400/30",
};

interface RunsPanelProps {
  workflow: ProductWorkflow | null;
  latestRun: ProductWorkflow["runs"][number] | null;
  busyRunId: string | null;
  onRetryRun: (run: WorkflowRun) => void;
}

function imagePromptItems(
  workflow: ProductWorkflow,
  run: WorkflowRun,
): Array<{ nodeId: string; title: string; instruction: string }> {
  return run.node_runs.flatMap((nodeRun) => {
    const node = workflow.nodes.find((item) => item.id === nodeRun.node_id);
    if (node?.node_type !== "image_generation" || !nodeRun.output_json) {
      return [];
    }
    const instruction = outputText(nodeRun.output_json, "instruction");
    if (!instruction) {
      return [];
    }
    return [
      {
        nodeId: node.id,
        title: node.title,
        instruction,
      },
    ];
  });
}

function findWorkflowNode(workflow: ProductWorkflow, nodeId: string): WorkflowNode | null {
  return workflow.nodes.find((node) => node.id === nodeId) ?? null;
}

export function RunsPanel({ workflow, latestRun, busyRunId, onRetryRun }: RunsPanelProps) {
  const { t } = useI18n();
  const [promptPreview, setPromptPreview] = useState<PromptPreview | null>(null);

  if (!workflow) {
    return (
      <section className="space-y-3">
        <div className="flex items-center justify-between">
          <div className="h-4 w-24 animate-shimmer" />
          <div className="h-5 w-28 rounded-full animate-shimmer" />
        </div>
        <div className="space-y-2">
          {[1, 2, 3].map((i) => (
            <div
              key={i}
              className="config-bubble space-y-3 rounded-xl p-3"
            >
              <div className="flex items-center gap-2">
                <div className="h-2.5 w-2.5 rounded-full animate-shimmer" />
                <div className="h-4 w-28 animate-shimmer" />
                <div className="ml-auto h-4 w-12 rounded animate-shimmer" />
              </div>
              <div className="space-y-1.5 pl-4.5">
                <div className="h-3 w-3/4 animate-shimmer" />
                <div className="h-3 w-1/2 animate-shimmer" />
              </div>
            </div>
          ))}
        </div>
      </section>
    );
  }

  return (
    <section>
      <div className="mb-3 flex items-center justify-between">
        <div className="text-xs text-zinc-500 dark:text-slate-400">
          {workflow?.runs.length ? t("detail.runsCount", { count: workflow.runs.length }) : t("detail.noRunHistory")}
        </div>
        {latestRun ? (
          <div className="rounded-full border border-zinc-200 bg-zinc-50 px-2.5 py-1 text-[11px] text-zinc-500 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-300">
            {t("detail.latest", { time: formatDateTime(latestRun.started_at, t.locale) })}
          </div>
        ) : null}
      </div>
      {workflow?.runs.length ? (
        <div className="space-y-2">
          {workflow.runs.map((run) => {
            const promptItems = imagePromptItems(workflow, run);
            const queueText = workflowRunQueueText(run, t);
            const runBusy = busyRunId === run.id;
            return (
              <div
                key={run.id}
                className="config-bubble rounded-xl p-3 text-xs shadow-sm"
              >
                <div className="space-y-3">
                  <div className="flex min-w-0 items-start gap-3">
                    <span
                      className={`mt-1.5 h-2.5 w-2.5 shrink-0 rounded-full shadow-md ${RUN_STATUS_DOT_CLASS_NAMES[run.status]}`}
                    />
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <span
                          className={`rounded-full border px-2 py-0.5 text-[10px] font-medium ${RUN_STATUS_CLASS_NAMES[run.status]}`}
                        >
                          {t(`detail.runStatus.${run.status}`)}
                        </span>
                        <span className="inline-flex items-center text-[11px] text-zinc-500 dark:text-slate-400">
                          <Layers3 size={12} className="mr-1 text-zinc-400 dark:text-slate-500" />
                          {t("detail.nodeRunCount", { count: run.node_runs.length })}
                        </span>
                        {run.is_cancelable ? (
                          <span className="rounded-full border border-amber-200 bg-amber-50 px-2 py-0.5 text-[10px] font-medium text-amber-700 dark:border-amber-400/40 dark:bg-amber-500/12 dark:text-amber-100">
                            {t("detail.runCancelable")}
                          </span>
                        ) : null}
                        <span className="text-[11px] text-zinc-400 dark:text-slate-500">
                          {formatDateTime(run.started_at, t.locale)}
                        </span>
                        {run.finished_at ? (
                          <span className="text-[11px] text-zinc-400 dark:text-slate-500">
                            {t("detail.finished", { time: formatDateTime(run.finished_at, t.locale) })}
                          </span>
                        ) : null}
                      </div>
                      {queueText ? <div className="mt-2 text-[11px] leading-5 text-zinc-500 dark:text-slate-400">{queueText}</div> : null}
                    </div>
                    {run.is_retryable ? (
                      <button
                        type="button"
                        onClick={() => onRetryRun(run)}
                        disabled={runBusy}
                        className="inline-flex shrink-0 items-center rounded-lg border border-zinc-200 bg-white px-2 py-1 text-[11px] font-medium text-zinc-600 transition-colors hover:border-red-200 hover:bg-red-50 hover:text-red-700 disabled:opacity-60 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-300 dark:hover:border-red-400/50 dark:hover:bg-red-500/12 dark:hover:text-red-200"
                      >
                        {runBusy ? (
                          <Loader2 size={12} className="mr-1 animate-spin" />
                        ) : (
                          <RotateCcw size={12} className="mr-1" />
                        )}
                        {t("detail.retry")}
                      </button>
                    ) : null}
                  </div>
                  <div className="rounded-xl border border-zinc-100 bg-zinc-50/70 p-2 dark:border-slate-700/70 dark:bg-[#0b1220]/70">
                    <div className="mb-1.5 text-[10px] font-semibold uppercase tracking-widest text-zinc-400 dark:text-slate-500">
                      {t("detail.nodeRunDetails")}
                    </div>
                    <div className="divide-y divide-zinc-100 overflow-hidden rounded-lg border border-white bg-white dark:divide-slate-700 dark:border-slate-700 dark:bg-[#111b2d]">
                      {run.node_runs.map((nodeRun) => {
                        const node = findWorkflowNode(workflow, nodeRun.node_id);
                        const promptItem = promptItems.find((item) => item.nodeId === nodeRun.node_id);
                        const durationText = workflowNodeRunDurationText(nodeRun, t);
                        const providerSummary = workflowNodeRunProviderSummary(nodeRun, t);
                        return (
                          <div key={nodeRun.id} className="px-2.5 py-2">
                            <div className="flex items-start justify-between gap-2">
                              <div className="min-w-0">
                                <div className="flex flex-wrap items-center gap-1.5">
                                  <span className="max-w-[170px] truncate text-[11px] font-semibold text-zinc-800 dark:text-slate-100">
                                    {node ? workflowNodeDisplayTitle(node, t) : t("detail.nodeRunUnknown")}
                                  </span>
                                  {node ? (
                                    <span className="text-[10px] text-zinc-400 dark:text-slate-500">
                                      {workflowNodeDisplayLabel(node, t)}
                                    </span>
                                  ) : null}
                                  {durationText ? (
                                    <span className="text-[10px] text-zinc-400 dark:text-slate-500">{durationText}</span>
                                  ) : null}
                                </div>
                                {providerSummary ? (
                                  <div className="mt-1 line-clamp-2 text-[10px] leading-4 text-zinc-500 dark:text-slate-400">
                                    {providerSummary}
                                  </div>
                                ) : null}
                              </div>
                              <span
                                className={`shrink-0 rounded-full border px-2 py-0.5 text-[10px] font-medium ${statusClass(nodeRun.status)}`}
                              >
                                {workflowNodeRunStatusLabel(nodeRun.status, t)}
                              </span>
                            </div>
                            {nodeRun.failure_reason ? (
                              <div
                                className={`mt-2 line-clamp-2 rounded-lg border px-2 py-1 text-[11px] leading-5 ${
                                  nodeRun.status === "cancelled"
                                    ? "border-zinc-100 bg-zinc-50 text-zinc-600 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-300"
                                    : "border-red-100 bg-red-50 text-red-700 dark:border-red-400/35 dark:bg-red-500/10 dark:text-red-200"
                                }`}
                              >
                                {nodeRun.failure_reason}
                              </div>
                            ) : null}
                            {promptItem ? (
                              <button
                                type="button"
                                onClick={() =>
                                  setPromptPreview({
                                    title: `${promptItem.title} Prompt`,
                                    text: promptItem.instruction,
                                    meta: formatDateTime(run.started_at, t.locale),
                                  })
                                }
                                className="mt-2 inline-flex max-w-full items-center rounded-lg border border-zinc-200 bg-zinc-50 px-2 py-1 text-[11px] font-medium text-zinc-600 transition-colors hover:border-indigo-200 hover:bg-indigo-50 hover:text-indigo-700 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-300 dark:hover:border-violet-400/50 dark:hover:bg-violet-500/12 dark:hover:text-violet-100"
                              >
                                <FileText size={12} className="mr-1 shrink-0" />
                                <span className="truncate">{t("detail.nodeRunPrompt")}</span>
                              </button>
                            ) : null}
                          </div>
                        );
                      })}
                    </div>
                  </div>
                  {run.failure_reason ? (
                    <div
                      className={`line-clamp-2 rounded-lg border px-2.5 py-1.5 ${
                        run.status === "cancelled"
                          ? "border-zinc-100 bg-zinc-50 text-zinc-600 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-300"
                          : "border-red-100 bg-red-50 text-red-700 dark:border-red-400/35 dark:bg-red-500/10 dark:text-red-200"
                      }`}
                    >
                      {run.failure_reason}
                    </div>
                  ) : null}
                  {run.status === "failed" && !run.is_retryable ? (
                    <div className="inline-flex rounded-lg border border-red-100 bg-white px-2.5 py-1 text-[11px] font-medium text-red-600 dark:border-red-400/35 dark:bg-[#0b1220] dark:text-red-200">
                      {t("detail.notRetryable")}
                    </div>
                  ) : null}
                </div>
              </div>
            );
          })}
        </div>
      ) : (
        <div className="flex min-h-[160px] items-center justify-center rounded-xl border border-dashed border-zinc-200 bg-zinc-50/60 px-4 py-6 text-center text-xs text-zinc-500 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-400">
          {t("detail.noRuns")}
        </div>
      )}
      {promptPreview ? (
        <PromptPreviewDialog preview={promptPreview} onClose={() => setPromptPreview(null)} />
      ) : null}
    </section>
  );
}
