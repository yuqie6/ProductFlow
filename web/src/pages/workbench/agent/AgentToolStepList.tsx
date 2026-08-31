import {
  BookOpen,
  ChevronDown,
  CircleCheck,
  CircleHelp,
  CircleX,
  FileClock,
  FilePen,
  FileSearch,
  FolderCog,
  GitBranch,
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

const SKILL_LABEL_KEYS: Record<string, TranslationKey> = {
  "graph-editing": "agentWorkbench.toolStep.skill.graphEditing",
  "media-library-organization": "agentWorkbench.toolStep.skill.mediaLibraryOrganization",
  "library-organization": "agentWorkbench.toolStep.skill.mediaLibraryOrganization",
  "product-intake": "agentWorkbench.toolStep.skill.productIntake",
  "run-diagnosis": "agentWorkbench.toolStep.skill.runDiagnosis",
  "workflow-run-request": "agentWorkbench.toolStep.skill.workflowRunRequest",
};

function skillLabelKey(name?: string): TranslationKey | undefined {
  if (!name) return undefined;
  return SKILL_LABEL_KEYS[name];
}

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
  apply_graph: "agentWorkbench.toolStep.kind.applyGraph",
  propose_graph: "agentWorkbench.toolStep.kind.proposeGraph",
  focus_canvas: "agentWorkbench.toolStep.kind.focusCanvas",
  expand_intake: "agentWorkbench.toolStep.kind.expandIntake",
  discard_proposal: "agentWorkbench.toolStep.kind.discardProposal",
  cancel_run: "agentWorkbench.toolStep.kind.cancelRun",
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
  apply_graph: GitBranch,
  propose_graph: FilePen,
  focus_canvas: ScanEye,
  expand_intake: Layers3,
  discard_proposal: CircleX,
  cancel_run: CircleX,
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
      {steps.map((step) => (
        <li key={step.step_id} className="min-w-0">
          <AgentToolStepRow step={step} />
        </li>
      ))}
    </ul>
  );
}

export function AgentToolStepRow({ step }: { step: AgentToolStep }) {
  const { t } = useI18n();
  const kindKey = KIND_KEYS[step.kind];
  const statusKey = STATUS_KEYS[step.status];
  const KindIcon = KIND_ICONS[step.kind] ?? CircleHelp;
  const StatusIcon = STATUS_ICONS[step.status] ?? CircleHelp;
  const kindLabel = kindKey ? t(kindKey) : t("agentWorkbench.toolStep.kind.unknown");
  const statusLabel = statusKey ? t(statusKey) : t("agentWorkbench.status.unknown");
  const mappedSkillKey = skillLabelKey(step.details?.skill_name);
  const skillLabel = mappedSkillKey ? t(mappedSkillKey) : null;
  const title = skillLabel ? `${kindLabel} · ${skillLabel} · ${statusLabel}` : `${kindLabel} · ${statusLabel}`;
  const tone = STATUS_TONES[step.status] ?? "text-text-muted";
  const hasDetails = hasUserVisibleDetails(step.details);
  const questionPreview =
    step.kind === "ask_question" && step.status !== "running" && step.details?.output_summary
      ? step.details.output_summary
      : null;
  const row = (
    <div className="flex min-h-8 min-w-0 items-center gap-2 rounded-lg px-2 py-1 text-xs transition-colors hover:bg-surface-subtle">
      <KindIcon size={14} className="shrink-0 text-text-muted" aria-hidden="true" />
      <span className="min-w-0 flex-1 truncate font-medium text-text-secondary">{kindLabel}</span>
      {skillLabel || questionPreview ? (
        <span className="max-w-[45%] truncate leading-5 text-text-secondary">{skillLabel ?? questionPreview}</span>
      ) : null}
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
    <div
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
    </div>
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
  const mappedSkillKey = skillLabelKey(details.skill_name);
  return (
    <div
      data-agent-tool-step-details
      className="ml-8 grid gap-1 border-l border-border-l2 py-2 pl-3 text-[11px] leading-5 text-text-muted"
    >
      {mappedSkillKey ? (
        <DetailLine label={t("agentWorkbench.toolStep.detail.skill")} value={t(mappedSkillKey)} />
      ) : null}
      {details.output_summary ? (
        <DetailLine label={t("agentWorkbench.toolStep.detail.output")} value={details.output_summary} />
      ) : null}
      {details.pending_confirmation ? (
        <span>{t("agentWorkbench.toolStep.detail.pending")}</span>
      ) : null}
      {details.reconciled ? <span>{t("agentWorkbench.toolStep.detail.reconciled")}</span> : null}
      {details.workflow_title ? (
        <DetailLine label={t("agentWorkbench.toolStep.detail.workflow")} value={details.workflow_title} />
      ) : null}
      {details.node_count !== undefined ? (
        <DetailLine label={t("agentWorkbench.toolStep.detail.nodeCount")} value={String(details.node_count)} />
      ) : null}
      {details.group_count !== undefined ? (
        <DetailLine label={t("agentWorkbench.toolStep.detail.groupCount")} value={String(details.group_count)} />
      ) : null}
      {details.item_count !== undefined ? (
        <DetailLine label={t("agentWorkbench.toolStep.detail.itemCount")} value={String(details.item_count)} />
      ) : null}
      {details.asset_count !== undefined ? (
        <DetailLine label={t("agentWorkbench.toolStep.detail.assetCount")} value={String(details.asset_count)} />
      ) : null}
      {details.question_text ? (
        <DetailLine label={t("agentWorkbench.toolStep.detail.question")} value={details.question_text} />
      ) : null}
      {details.option_labels?.length ? (
        <DetailLine label={t("agentWorkbench.toolStep.detail.options")} value={details.option_labels.join(" · ")} />
      ) : null}
      {details.error_code || details.error_message ? (
        <div className="grid gap-0.5 text-state-error">
          <span className="font-semibold">{t("agentWorkbench.toolStep.detail.error")}</span>
          <span>{t("agentWorkbench.toolStep.detail.failureMessage")}</span>
        </div>
      ) : null}
      {details.validation_issues?.length ? (
        <div className="grid gap-1 text-state-error">
          <span className="font-semibold">{t("agentWorkbench.toolStep.detail.validation")} </span>
          <span>{t("agentWorkbench.toolStep.detail.validationMessage")}</span>
        </div>
      ) : null}
      {details.retryable ? <span className="text-state-warning">{t("agentWorkbench.toolStep.detail.retryable")}</span> : null}
    </div>
  );
}

function hasUserVisibleDetails(details?: AgentToolStepDetails): boolean {
  if (!details) return false;
  return Boolean(
    skillLabelKey(details.skill_name)
    || details.output_summary
    || details.pending_confirmation
    || details.reconciled
    || details.workflow_title
    || details.node_count !== undefined
    || details.group_count !== undefined
    || details.item_count !== undefined
    || details.asset_count !== undefined
    || details.question_text
    || details.option_labels?.length
    || details.error_code
    || details.error_message
    || details.validation_issues?.length
    || details.retryable,
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
