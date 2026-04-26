import { Layers3 } from "lucide-react";

import { formatDateTime } from "../../lib/format";
import type { ProductWorkflow, WorkflowRunStatus } from "../../lib/types";

const RUN_STATUS_LABELS: Record<WorkflowRunStatus, string> = {
  running: "运行中",
  succeeded: "成功",
  failed: "失败",
};

const RUN_STATUS_CLASS_NAMES: Record<WorkflowRunStatus, string> = {
  running: "border-blue-200 bg-blue-50 text-blue-700",
  succeeded: "border-emerald-200 bg-emerald-50 text-emerald-700",
  failed: "border-red-200 bg-red-50 text-red-700",
};

const RUN_STATUS_DOT_CLASS_NAMES: Record<WorkflowRunStatus, string> = {
  running: "bg-blue-500 shadow-blue-500/30",
  succeeded: "bg-emerald-500 shadow-emerald-500/30",
  failed: "bg-red-500 shadow-red-500/30",
};

interface RunsPanelProps {
  workflow: ProductWorkflow | null;
  latestRun: ProductWorkflow["runs"][number] | null;
}

export function RunsPanel({ workflow, latestRun }: RunsPanelProps) {
  return (
    <section>
      <div className="mb-3 flex items-center justify-between">
        <div className="text-xs text-zinc-500">
          {workflow?.runs.length ? `共 ${workflow.runs.length} 次运行` : "暂无运行历史"}
        </div>
        {latestRun ? (
          <div className="rounded-full border border-zinc-200 bg-zinc-50 px-2.5 py-1 text-[11px] text-zinc-500">
            最近 {formatDateTime(latestRun.started_at)}
          </div>
        ) : null}
      </div>
      {workflow?.runs.length ? (
        <div className="space-y-2">
          {workflow.runs.map((run) => (
            <div
              key={run.id}
              className="rounded-xl border border-zinc-200 bg-white px-3 py-2.5 text-xs shadow-sm"
            >
              <div className="flex items-start justify-between gap-3">
                <div className="flex min-w-0 items-start gap-3">
                  <span
                    className={`mt-1.5 h-2.5 w-2.5 shrink-0 rounded-full shadow-md ${RUN_STATUS_DOT_CLASS_NAMES[run.status]}`}
                  />
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <span
                        className={`rounded-full border px-2 py-0.5 text-[10px] font-medium ${RUN_STATUS_CLASS_NAMES[run.status]}`}
                      >
                        {RUN_STATUS_LABELS[run.status]}
                      </span>
                      <span className="inline-flex items-center text-[11px] text-zinc-500">
                        <Layers3 size={12} className="mr-1 text-zinc-400" />
                        节点记录 {run.node_runs.length}
                      </span>
                    </div>
                    {run.failure_reason ? (
                      <div className="mt-2 line-clamp-2 rounded-lg border border-red-100 bg-red-50 px-2.5 py-1.5 text-red-700">
                        {run.failure_reason}
                      </div>
                    ) : null}
                  </div>
                </div>
                <div className="shrink-0 text-right text-[10px] leading-relaxed text-zinc-400">
                  <div>{formatDateTime(run.started_at)}</div>
                  {run.finished_at ? <div>完成 {formatDateTime(run.finished_at)}</div> : null}
                </div>
              </div>
            </div>
          ))}
        </div>
      ) : (
        <div className="flex min-h-[160px] items-center justify-center rounded-xl border border-dashed border-zinc-200 bg-zinc-50/60 px-4 py-6 text-center text-xs text-zinc-500">
          暂无运行记录
        </div>
      )}
    </section>
  );
}
