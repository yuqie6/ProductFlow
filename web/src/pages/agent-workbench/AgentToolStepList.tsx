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

const STATUS_TONES: Record<AgentToolStepStatus, string> = {
  running: "text-accent",
  succeeded: "text-state-success",
  failed: "text-state-error",
  unknown: "text-state-warning",
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
      className="mt-2 space-y-1"
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
        return (
          <li
            key={step.step_id}
            data-agent-tool-step-id={step.step_id}
            data-agent-tool-step-status={step.status}
            title={title}
            className="flex min-h-8 items-center gap-2 border-l-2 border-border-l2 px-2 py-1 text-xs"
          >
            <KindIcon size={14} className="shrink-0 text-text-secondary" />
            <span className="shrink-0 font-medium text-text-secondary">{kindLabel}</span>
            <span className="min-w-0 flex-1 truncate text-text-primary">{step.summary}</span>
            <span className={`inline-flex shrink-0 items-center gap-1 ${STATUS_TONES[step.status]}`}>
              <StatusIcon
                size={13}
                className={step.status === "running" ? "animate-spin motion-reduce:animate-none" : ""}
              />
              <span className="font-medium">{statusLabel}</span>
            </span>
          </li>
        );
      })}
    </ul>
  );
}
