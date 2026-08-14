import { useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bot, X } from "lucide-react";
import { useNavigate } from "react-router-dom";

import { api, ApiError } from "../lib/api";
import type { TranslateFunction } from "../lib/preferences";
import { useI18n } from "../lib/preferences";
import type { AgentProductImageTypeKey } from "../lib/types";
import { AgentProductCreateForm } from "./product-create/AgentProductCreateForm";
import {
  buildAgentProductSelection,
  toggleAgentImageType,
  updateAgentImageTypeQuantity,
  validateAgentProductWorkspaceInput,
  type AgentImageTypeSelectionDraft,
  type AgentProductCreateValidationIssue,
} from "./product-create/imageTypeSelection";

function createIdempotencyKey(): string {
  return globalThis.crypto.randomUUID();
}

function validationMessage(t: TranslateFunction, issue: AgentProductCreateValidationIssue): string {
  switch (issue.code) {
    case "options_unavailable":
      return t("agentCreate.error.optionsUnavailable");
    case "name_required":
      return t("agentCreate.error.nameRequired");
    case "image_type_required":
      return t("agentCreate.error.imageTypeRequired", { minimum: issue.minimum });
    case "duplicate_image_type":
      return t("agentCreate.error.duplicateImageType");
    case "quantity_out_of_range":
      return t("agentCreate.error.quantityRange", {
        minimum: issue.minimum,
        maximum: issue.maximum,
      });
    case "total_images_exceeded":
      return t("agentCreate.error.totalExceeded", { total: issue.total, maximum: issue.maximum });
    case "reference_count_out_of_range":
      return t("agentCreate.error.referenceRange", {
        minimum: issue.minimum,
        maximum: issue.maximum,
      });
  }
}

export function AgentProductCreatePage() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const idempotencyKeyRef = useRef(createIdempotencyKey());
  const [name, setName] = useState("");
  const [selections, setSelections] = useState<AgentImageTypeSelectionDraft[]>([]);
  const [referenceFiles, setReferenceFiles] = useState<File[]>([]);
  const [error, setError] = useState("");

  const optionsQuery = useQuery({
    queryKey: ["agent-product-workspace-options"],
    queryFn: api.getAgentProductWorkspaceOptions,
  });
  const options = optionsQuery.data ?? null;

  const rotateIdempotencyKey = () => {
    idempotencyKeyRef.current = createIdempotencyKey();
  };

  const createMutation = useMutation({
    mutationFn: api.createAgentProductWorkspace,
    onSuccess: async (creation) => {
      await queryClient.invalidateQueries({ queryKey: ["products"] });
      navigate(`/products/${creation.product.id}`);
    },
    onError: (mutationError) => {
      setError(
        mutationError instanceof ApiError
          ? mutationError.detail
          : mutationError instanceof Error
            ? mutationError.message
            : t("agentCreate.error.failed"),
      );
    },
  });

  const handleSubmit = () => {
    if (createMutation.isPending) {
      return;
    }
    const issue = validateAgentProductWorkspaceInput({
      name,
      selections,
      referenceImageCount: referenceFiles.length,
      limits: options?.limits ?? null,
    });
    if (issue) {
      setError(validationMessage(t, issue));
      return;
    }
    setError("");
    createMutation.mutate({
      name: name.trim(),
      selection: buildAgentProductSelection(selections),
      images: referenceFiles,
      idempotency_key: idempotencyKeyRef.current,
    });
  };

  const handleToggleImageType = (key: AgentProductImageTypeKey, selected: boolean) => {
    if (!options) {
      return;
    }
    setSelections((current) =>
      toggleAgentImageType(current, key, selected, options.limits.default_images_per_type),
    );
    rotateIdempotencyKey();
    setError("");
  };

  const handleQuantityChange = (key: AgentProductImageTypeKey, quantity: number) => {
    setSelections((current) => updateAgentImageTypeQuantity(current, key, quantity));
    rotateIdempotencyKey();
    setError("");
  };

  const handleAddReferenceFiles = (files: File[]) => {
    if (!options) {
      setError(t("agentCreate.error.optionsUnavailable"));
      return;
    }
    if (
      files.some(
        (file) => !options.limits.allowed_image_mime_types.some((mimeType) => mimeType === file.type),
      )
    ) {
      setError(t("agentCreate.error.unsupportedImage"));
      return;
    }
    if (referenceFiles.length + files.length > options.limits.max_reference_images) {
      setError(
        t("agentCreate.error.tooManyReferences", { maximum: options.limits.max_reference_images }),
      );
      return;
    }
    setReferenceFiles((current) => [...current, ...files]);
    rotateIdempotencyKey();
    setError("");
  };

  const handleRemoveReferenceFile = (index: number) => {
    setReferenceFiles((current) => current.filter((_, currentIndex) => currentIndex !== index));
    rotateIdempotencyKey();
    setError("");
  };

  const liveIssue = options
    ? validateAgentProductWorkspaceInput({
        name: name || "pending-name",
        selections,
        referenceImageCount: Math.max(referenceFiles.length, options.limits.min_reference_images),
        limits: options.limits,
      })
    : null;
  const liveError =
    error ||
    (liveIssue?.code === "quantity_out_of_range" || liveIssue?.code === "total_images_exceeded"
      ? validationMessage(t, liveIssue)
      : "");

  return (
    <div className="min-h-screen bg-[#f5f6f7] text-zinc-900 dark:bg-[#060a12] dark:text-slate-100">
      <header className="border-b border-zinc-200 bg-white dark:border-slate-800 dark:!bg-[#090d13]">
        <div className="mx-auto flex min-h-20 max-w-[1320px] items-center justify-between gap-4 px-4 py-4 sm:px-6 lg:px-8">
          <div className="flex min-w-0 items-center gap-3">
            <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-md bg-blue-600 text-white dark:bg-cyan-400 dark:text-[#071018]">
              <Bot size={20} />
            </div>
            <div className="min-w-0">
              <h1 className="truncate text-lg font-semibold text-zinc-950 dark:text-white">{t("agentCreate.title")}</h1>
              <p className="mt-0.5 truncate text-sm text-zinc-500 dark:text-slate-400">{t("agentCreate.description")}</p>
            </div>
          </div>
          <button
            type="button"
            title={t("agentCreate.close")}
            aria-label={t("agentCreate.close")}
            onClick={() => navigate("/products")}
            className="flex h-10 w-10 shrink-0 items-center justify-center rounded-md border border-zinc-200 bg-white text-zinc-500 hover:border-zinc-400 hover:text-zinc-950 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-600 dark:border-slate-700 dark:!bg-[#0d1117] dark:text-slate-400 dark:hover:text-white"
          >
            <X size={18} />
          </button>
        </div>
      </header>

      <main className="px-4 py-7 sm:px-6 lg:px-8 lg:py-10">
        <AgentProductCreateForm
          options={options}
          name={name}
          selections={selections}
          referenceFiles={referenceFiles}
          isOptionsLoading={optionsQuery.isLoading}
          isOptionsError={optionsQuery.isError}
          isSubmitting={createMutation.isPending}
          error={liveError}
          onNameChange={(nextName) => {
            setName(nextName);
            rotateIdempotencyKey();
            setError("");
          }}
          onToggleImageType={handleToggleImageType}
          onQuantityChange={handleQuantityChange}
          onAddReferenceFiles={handleAddReferenceFiles}
          onRemoveReferenceFile={handleRemoveReferenceFile}
          onRetryOptions={() => void optionsQuery.refetch()}
          onCancel={() => navigate("/products")}
          onSubmit={handleSubmit}
        />
      </main>
    </div>
  );
}
