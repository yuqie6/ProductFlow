import {
  BookOpen,
  CircleCheck,
  CircleHelp,
  CircleX,
  ChevronDown,
  FileClock,
  FileSearch,
  FolderCog,
  Layers3,
  LoaderCircle,
  MessageCircleQuestion,
  PackagePlus,
  ScanEye,
  ScrollText,
  Workflow,
} from "lucide-react";
import { useState, type ComponentType, type ReactNode } from "react";

import type { TranslationKey } from "../../../lib/i18n";
import { useI18n } from "../../../lib/preferences";
import type { AgentToolStep, AgentToolStepDetails, AgentToolStepKind, AgentToolStepStatus } from "../../../lib/types";

const KIND_KEYS: Record<AgentToolStepKind, TranslationKey> = {
  load_skill: "agentWorkbench.toolStep.kind.loadSkill",
  inject_context: "agentWorkbench.toolStep.kind.injectContext",
  ask_question: "agentWorkbench.toolStep.kind.askQuestion",
  inspect_image: "agentWorkbench.toolStep.kind.inspectImage",
  propose_draft: "agentWorkbench.toolStep.kind.proposeDraft",
  inspect_context: "agentWorkbench.toolStep.kind.inspectContext",
  read_history: "agentWorkbench.toolStep.kind.readHistory",
  organize_assets: "agentWorkbench.toolStep.kind.organizeAssets",
  request_workflow_run: "agentWorkbench.toolStep.kind.requestWorkflowRun",
  create_product: "agentWorkbench.toolStep.kind.createProduct",
};

const KIND_ICONS: Record<AgentToolStepKind, ComponentType<{ size?: number; className?: string }>> = {
  load_skill: BookOpen,
  inject_context: Layers3,
  ask_question: MessageCircleQuestion,
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
  unknown: "text-text-muted",
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
      className="mt-4 space-y-1 border-l border-border-l2 pl-3"
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
        const tone = STATUS_TONES[step.status];
        const hasDetails = Boolean(step.tool_name || step.details && Object.keys(step.details).length > 0);
        const row = (
          <div className="flex min-h-8 min-w-0 items-center gap-2 rounded-lg px-2 py-1 text-xs transition-colors hover:bg-surface-subtle">
            <KindIcon size={14} className="shrink-0 text-text-muted" aria-hidden="true" />
            <span className="shrink-0 font-medium text-text-secondary">{kindLabel}</span>
            {step.tool_name ? (
              <code className="max-w-[12rem] shrink truncate rounded bg-surface-subtle px-1 font-mono text-[10px] text-text-muted">
                {step.tool_name}
              </code>
            ) : null}
            <span className="min-w-0 flex-1 truncate leading-5 text-text-secondary">{step.summary}</span>
            <span className={`inline-flex shrink-0 items-center gap-1 text-[10px] font-medium ${tone}`}>
              <StatusIcon
                size={12}
                className={step.status === "running" ? "animate-spin motion-reduce:animate-none" : ""}
                aria-hidden="true"
              />
              <span>{statusLabel}</span>
            </span>
            {hasDetails ? <ChevronDown size={13} className="shrink-0 text-text-muted transition-transform group-open/details:rotate-180" aria-hidden="true" /> : null}
          </div>
        );

        return (
          <li
            key={step.step_id}
            data-agent-tool-step-id={step.step_id}
            data-agent-tool-step-status={step.status}
            title={title}
            className="group min-w-0 text-xs"
          >
            {hasDetails ? (
              <ToolStepDisclosure
                defaultOpen={step.status === "failed" && Boolean(step.details?.validation_issues?.length)}
                title={title}
                summary={row}
              >
                <ToolStepDetailsView details={step.details} />
              </ToolStepDisclosure>
            ) : (
              <div title={title}>{row}</div>
            )}
          </li>
        );
      })}
    </ul>
  );
}

function ToolStepDisclosure({
  defaultOpen,
  title,
  summary,
  children,
}: {
  defaultOpen: boolean;
  title: string;
  summary: ReactNode;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <details
      open={open}
      onToggle={(event) => setOpen(event.currentTarget.open)}
      className="group/details min-w-0"
    >
      <summary className="list-none cursor-pointer [&::-webkit-details-marker]:hidden" title={title}>
        {summary}
      </summary>
      {children}
    </details>
  );
}

function ToolStepDetailsView({ details }: { details?: AgentToolStepDetails }) {
  const { t } = useI18n();
  if (!details) return null;
  return (
    <div
      data-agent-tool-step-details
      className="ml-8 grid gap-1 border-l border-border-l2 py-2 pl-3 text-[11px] leading-5 text-text-muted"
    >
      {details.input_summary ? (
        <DetailLine label={t("agentWorkbench.toolStep.detail.input")} value={details.input_summary} />
      ) : null}
      {details.output_summary ? (
        <DetailLine label={t("agentWorkbench.toolStep.detail.output")} value={details.output_summary} />
      ) : null}
      {details.skill_name ? (
        <DetailLine label={t("agentWorkbench.toolStep.detail.skill")} value={details.skill_name} code />
      ) : null}
      {details.resource_path ? (
        <DetailLine label={t("agentWorkbench.toolStep.detail.resource")} value={details.resource_path} code />
      ) : null}
      {details.instruction_excerpt ? (
        <div className="grid min-w-0 gap-1">
          <span className="font-medium text-text-secondary">
            {t("agentWorkbench.toolStep.detail.instructions")}
            {details.instruction_truncated ? ` · ${t("agentWorkbench.toolStep.detail.truncated")}` : ""}
          </span>
          <pre
            data-agent-tool-step-instructions
            className="max-h-48 min-w-0 overflow-auto whitespace-pre-wrap break-words rounded bg-surface-subtle p-2 font-mono text-[10px] leading-4"
          >
            {details.instruction_excerpt}
          </pre>
        </div>
      ) : null}
      {details.question_text ? (
        <DetailLine label={t("agentWorkbench.toolStep.detail.question")} value={details.question_text} />
      ) : null}
      {details.option_labels?.length ? (
        <DetailLine label={t("agentWorkbench.toolStep.detail.options")} value={details.option_labels.join(" · ")} />
      ) : null}
      {details.context_sections?.length ? (
        <DetailLine label={t("agentWorkbench.toolStep.detail.context")} value={details.context_sections.join(" · ")} code />
      ) : null}
      {details.page_route ? <DetailLine label={t("agentWorkbench.toolStep.detail.page")} value={details.page_route} code /> : null}
      {details.page_type ? <DetailLine label={t("agentWorkbench.toolStep.detail.pageType")} value={details.page_type} /> : null}
      {details.context_bytes !== undefined ? (
        <DetailLine label={t("agentWorkbench.toolStep.detail.contextSize")} value={`${details.context_bytes} B`} />
      ) : null}
      {details.error_code || details.error_message ? (
        <div className="grid gap-0.5 text-state-error">
          <span className="font-semibold">{t("agentWorkbench.toolStep.detail.error")}</span>
          {details.error_code ? <code className="font-mono">{details.error_code}</code> : null}
          {details.error_message ? <span className="break-words">{details.error_message}</span> : null}
        </div>
      ) : null}
      {details.validation_issues?.length ? (
        <div className="grid gap-1 text-state-error">
          <span className="font-semibold">{t("agentWorkbench.toolStep.detail.validation")} </span>
          <ul className="grid gap-1 pl-3">
            {details.validation_issues.map((issue) => (
              <li key={`${issue.path}:${issue.message}`} className="break-words">
                <code className="font-mono">{issue.path}</code>: {issue.message}
              </li>
            ))}
          </ul>
        </div>
      ) : null}
      {details.retryable ? <span className="text-state-warning">{t("agentWorkbench.toolStep.detail.retryable")}</span> : null}
    </div>
  );
}

function DetailLine({ label, value, code = false }: { label: string; value: string; code?: boolean }) {
  return (
    <div className="grid min-w-0 grid-cols-[auto_minmax(0,1fr)] gap-2">
      <span className="font-medium text-text-secondary">{label}</span>
      {code ? <code className="min-w-0 break-words font-mono">{value}</code> : <span className="min-w-0 break-words">{value}</span>}
    </div>
  );
}
