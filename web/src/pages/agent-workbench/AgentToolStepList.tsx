import {
  CircleCheck,
  CircleHelp,
  CircleX,
  FileClock,
  FileSearch,
  FolderCog,
  ScanEye,
  LoaderCircle,
  PackagePlus,
  ScrollText,
  Workflow,
} from "lucide-react";
import type { ComponentType } from "react";

import type { TranslationKey } from "../../lib/i18n";
import { useI18n } from "../../lib/preferences";
import type { AgentToolStep, AgentToolStepKind, AgentToolStepStatus } from "../../lib/types";

const KIND_KEYS: Record<AgentToolStepKind, TranslationKey> = {
  inspect_image: "agentWorkbench.toolStep.kind.inspectImage",
  propose_draft: "agentWorkbench.toolStep.kind.proposeDraft",
  inspect_context: "agentWorkbench.toolStep.kind.inspectContext",
  read_history: "agentWorkbench.toolStep.kind.readHistory",
  organize_assets: "agentWorkbench.toolStep.kind.organizeAssets",
  request_workflow_run: "agentWorkbench.toolStep.kind.requestWorkflowRun",
  create_product: "agentWorkbench.toolStep.kind.createProduct",
};

const KIND_ICONS: Record<AgentToolStepKind, ComponentType<{ size?: number; className?: string }>> = {
  inspect_image: ScanEye,
  propose_draft: ScrollText,
  inspect_context: FileSearch,
  read_history: FileClock,
  organize_assets: FolderCog,
  request_workflow_run: Workflow,
  create_product: PackagePlus,
};

const STATUS_KEYS: Record<AgentToolStepStatus, TranslationKey> = {
  running: "agentWorkbench.status.running",
  succeeded: "agentWorkbench.status.succeeded",
  failed: "agentWorkbench.status.failed",
  unknown: "agentWorkbench.status.unknown",
};

const STATUS_ICONS: Record<AgentToolStepStatus, ComponentType<{ size?: number; className?: string }>> = {
  running: LoaderCircle,
  succeeded: CircleCheck,
  failed: CircleX,
  unknown: CircleHelp,
};

const STATUS_TONES: Record<AgentToolStepStatus, {
  container: string;
  badge: string;
  icon: string;
}> = {
  running: {
    container: "border-blue-200 bg-blue-50/60 text-blue-950 dark:border-cyan-800/50 dark:bg-cyan-950/25 dark:text-cyan-100",
    badge: "bg-blue-100/80 text-blue-700 dark:bg-cyan-900/60 dark:text-cyan-300",
    icon: "text-blue-600 dark:text-cyan-400",
  },
  succeeded: {
    container: "border-emerald-200/70 bg-emerald-50/40 text-zinc-900 dark:border-emerald-900/40 dark:bg-emerald-950/20 dark:text-slate-200",
    badge: "bg-emerald-100/70 text-emerald-800 dark:bg-emerald-900/50 dark:text-emerald-300",
    icon: "text-emerald-600 dark:text-emerald-400",
  },
  failed: {
    container: "border-red-200 bg-red-50/60 text-red-950 dark:border-red-900/50 dark:bg-red-950/25 dark:text-red-200",
    badge: "bg-red-100/80 text-red-700 dark:bg-red-900/60 dark:text-red-300",
    icon: "text-red-600 dark:text-red-400",
  },
  unknown: {
    container: "border-zinc-200 bg-zinc-50 text-zinc-700 dark:border-slate-800 dark:bg-slate-900/60 dark:text-slate-300",
    badge: "bg-zinc-100 text-zinc-600 dark:bg-slate-800 dark:text-slate-400",
    icon: "text-zinc-500 dark:text-slate-400",
  },
};

const KIND_TONES: Record<AgentToolStepKind, string> = {
  inspect_image: "bg-purple-100/80 text-purple-800 dark:bg-purple-950/60 dark:text-purple-300",
  propose_draft: "bg-indigo-100/80 text-indigo-800 dark:bg-indigo-950/60 dark:text-indigo-300",
  inspect_context: "bg-sky-100/80 text-sky-800 dark:bg-sky-950/60 dark:text-sky-300",
  read_history: "bg-slate-100/80 text-slate-800 dark:bg-slate-800/80 dark:text-slate-300",
  organize_assets: "bg-amber-100/80 text-amber-800 dark:bg-amber-950/60 dark:text-amber-300",
  request_workflow_run: "bg-cyan-100/80 text-cyan-800 dark:bg-cyan-950/60 dark:text-cyan-300",
  create_product: "bg-emerald-100/80 text-emerald-800 dark:bg-emerald-950/60 dark:text-emerald-300",
};

interface AgentToolStepListProps {
  steps: readonly AgentToolStep[];
  live?: boolean;
}

export function AgentToolStepList({ steps, live = false }: AgentToolStepListProps) {
  const { t } = useI18n();

  if (!steps.length) {
    return null;
  }

  return (
    <ul
      aria-label={t("agentWorkbench.toolStep.listLabel")}
      aria-live={live ? "polite" : undefined}
      className="mt-2 space-y-1.5"
    >
      {steps.map((step) => {
        const KindIcon = KIND_ICONS[step.kind];
        const StatusIcon = STATUS_ICONS[step.status];
        const kindLabel = t(KIND_KEYS[step.kind]);
        const statusLabel = t(STATUS_KEYS[step.status]);
        const title = t("agentWorkbench.toolStep.title", {
          kind: kindLabel,
          status: statusLabel,
          summary: step.summary,
        });
        const statusTone = STATUS_TONES[step.status];
        const kindTone = KIND_TONES[step.kind];

        return (
          <li
            key={step.step_id}
            data-agent-tool-step-id={step.step_id}
            data-agent-tool-step-status={step.status}
            title={title}
            className={`flex min-h-8 items-center gap-2 rounded-md border px-2.5 py-1.5 text-xs transition-colors ${statusTone.container}`}
          >
            <span className={`inline-flex shrink-0 items-center gap-1 rounded px-1.5 py-0.5 text-[11px] font-medium ${kindTone}`}>
              <KindIcon size={12} className="shrink-0" />
              <span>{kindLabel}</span>
            </span>
            <span className="min-w-0 flex-1 truncate font-normal leading-5">{step.summary}</span>
            <span className={`inline-flex shrink-0 items-center gap-1 rounded-full px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wider ${statusTone.badge}`}>
              <StatusIcon
                size={11}
                className={`${statusTone.icon} ${step.status === "running" ? "animate-spin motion-reduce:animate-none" : ""}`}
              />
              <span>{statusLabel}</span>
            </span>
          </li>
        );
      })}
    </ul>
  );
}
