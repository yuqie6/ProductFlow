import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FileStack, ImagePlus, RotateCw, X } from "lucide-react";
import { useNavigate } from "react-router-dom";

import { ConfirmDialog } from "../../components/ConfirmDialog";
import { ImageDropZone } from "../../components/ImageDropZone";
import { Button } from "../../components/ui/button";
import { IconButton } from "../../components/ui/icon-button";
import { api } from "../../lib/api";
import { useI18n } from "../../lib/preferences";
import { RecipeApplyPreviewBody } from "../workbench/canvas/RecipeLibraryPanel";
import { isAmbiguousFinalizeError } from "./intakeSubmission";

export function RecipeProductCreateForm() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [files, setFiles] = useState<File[]>([]);
  const [sourceNote, setSourceNote] = useState("");
  const [recipeId, setRecipeId] = useState("");
  const [open, setOpen] = useState(false);
  const [fileError, setFileError] = useState("");
  const key = useRef(crypto.randomUUID());
  const recipes = useQuery({
    queryKey: ["workflow-recipes", false],
    queryFn: () => api.listWorkflowRecipes(),
    retry: false,
  });
  const completeRecipes =
    recipes.data?.filter((recipe) => recipe.kind === "workflow_recipe") ?? [];
  const selected = completeRecipes.find((recipe) => recipe.id === recipeId);
  const version = selected?.current_version.version;
  const preview = useQuery({
    queryKey: ["recipe-creation-preview", recipeId, version],
    queryFn: () => api.previewRecipeCreation(recipeId, version!),
    enabled: open && version !== undefined,
    retry: false,
    staleTime: 0,
  });
  const create = useMutation({
    mutationFn: api.createProductFromRecipe,
    onSuccess: (result) => {
      queryClient.setQueryData(
        ["workflow-graph", result.product.id],
        result.graph,
      );
      queryClient.setQueryData(["product", result.product.id], result.product);
      void queryClient.invalidateQueries({ queryKey: ["products"] });
      navigate(`/products/${result.product.id}`);
    },
  });
  const changed = () => {
    key.current = crypto.randomUUID();
    create.reset();
  };
  const ready = Boolean(name.trim() && files.length > 0 && selected);
  const retryConfirmation =
    create.isError &&
    isAmbiguousFinalizeError(create.error) &&
    Boolean(create.variables);
  const fieldClass =
    "w-full min-w-0 rounded-md border border-border-l2 bg-surface-raised px-3 py-2 text-sm text-text-primary focus:outline-none focus:ring-2 focus:ring-accent";
  return (
    <form
      data-create-recipe-form
      className="space-y-5 pb-8"
      onSubmit={(event) => {
        event.preventDefault();
        if (ready) setOpen(true);
      }}
    >
      <div>
        <label
          htmlFor="recipe-product-name"
          className="mb-2 block text-sm font-medium"
        >
          {t("agentCreate.recipe.productName")}
        </label>
        <input
          id="recipe-product-name"
          required
          maxLength={255}
          value={name}
          className={fieldClass}
          onChange={(event) => {
            setName(event.target.value);
            changed();
          }}
        />
      </div>
      <div>
        <label
          htmlFor="create-recipe"
          className="mb-2 block text-sm font-medium"
        >
          {t("agentCreate.recipe.mode")}
        </label>
        <select
          id="create-recipe"
          className={fieldClass}
          value={recipeId}
          disabled={recipes.isPending}
          required
          onChange={(event) => {
            setRecipeId(event.target.value);
            changed();
          }}
        >
          <option value="">
            {recipes.isPending
              ? t("workbench.recipe.loading")
              : t("agentCreate.recipe.choose")}
          </option>
          {completeRecipes.map((recipe) => (
            <option key={recipe.id} value={recipe.id}>
              {recipe.current_version.title} · v{recipe.current_version.version}
            </option>
          ))}
        </select>
        {recipes.isError ? (
          <div
            role="alert"
            className="mt-2 flex items-center gap-2 text-sm text-state-error"
          >
            {recipes.error.message}
            <IconButton
              label={t("workbench.retry")}
              onClick={() => void recipes.refetch()}
            >
              <RotateCw size={16} />
            </IconButton>
          </div>
        ) : null}
        {!recipes.isPending &&
        !recipes.isError &&
        completeRecipes.length === 0 ? (
          <p className="mt-2 text-sm text-text-secondary">
            {t("agentCreate.recipe.empty")}
          </p>
        ) : null}
      </div>
      <div>
        <ImageDropZone
          multiple
          ariaLabel={t("agentCreate.recipe.upload")}
          disabled={files.length >= 6}
          className="flex min-h-24 cursor-pointer items-center justify-center gap-2 rounded-md border border-dashed border-border-l2 px-4 py-5 text-sm text-text-secondary"
          onFiles={(added) => {
            if (files.length + added.length > 6) {
              setFileError(
                t("agentCreate.error.tooManyReferences", { maximum: 6 }),
              );
              return;
            }
            if (
              added.some(
                (file) =>
                  !["image/png", "image/jpeg", "image/webp"].includes(
                    file.type,
                  ),
              )
            ) {
              setFileError(t("agentCreate.error.unsupportedImage"));
              return;
            }
            setFiles([...files, ...added]);
            setFileError("");
            changed();
          }}
        >
          <ImagePlus size={18} />
          {t("agentCreate.recipe.upload")}
        </ImageDropZone>
        <ul className="mt-3 grid grid-cols-1 gap-2 sm:grid-cols-2">
          {files.map((file, index) => (
            <li
              key={index}
              className="flex min-w-0 items-center gap-3 border-b border-border-l1 py-2"
            >
              <ReferenceThumbnail file={file} />
              <span
                className="min-w-0 flex-1 truncate text-xs"
                title={file.name}
              >
                {file.name}
              </span>
              <IconButton
                label={t("agentCreate.remove", { name: file.name })}
                onClick={() => {
                  setFiles(files.filter((_, i) => i !== index));
                  setFileError("");
                  changed();
                }}
              >
                <X size={15} />
              </IconButton>
            </li>
          ))}
        </ul>
        {fileError ? (
          <p role="alert" className="text-sm text-state-error">
            {fileError}
          </p>
        ) : null}
      </div>
      <div>
        <label
          htmlFor="recipe-source-note"
          className="mb-2 block text-sm font-medium"
        >
          {t("agentCreate.recipe.sourceNote")}
        </label>
        <textarea
          id="recipe-source-note"
          rows={4}
          maxLength={4000}
          className={fieldClass}
          value={sourceNote}
          onChange={(event) => {
            setSourceNote(event.target.value);
            changed();
          }}
        />
      </div>
      <Button type="submit" disabled={!ready} className="w-full sm:w-auto">
        <FileStack size={16} />
        {t("agentCreate.recipe.preview")}
      </Button>
      <ConfirmDialog
        open={open}
        title={t("workbench.recipe.previewTitle")}
        description=""
        body={
          <div className="max-h-[55dvh] space-y-3 overflow-y-auto break-words">
            <p className="text-sm font-medium">{name}</p>
            <p className="text-sm text-text-secondary">{selected?.current_version.title}</p>
            {preview.isFetching ? (
              <p role="status">{t("workbench.recipe.previewLoading")}</p>
            ) : preview.isError ? (
              <p role="alert" className="text-state-error">
                {preview.error.message}
              </p>
            ) : preview.data ? (
              <RecipeApplyPreviewBody preview={preview.data} />
            ) : null}
            {create.isError ? (
              <p role="alert" className="mt-3 text-sm text-state-error">
                {create.error.message}
              </p>
            ) : null}
          </div>
        }
        confirmLabel={t("agentCreate.recipe.confirm")}
        cancelLabel={t("common.cancel")}
        destructive={false}
        busy={create.isPending}
        confirmDisabled={
          !retryConfirmation &&
          (preview.isFetching || preview.isError || !preview.data)
        }
        onClose={() => {
          if (!create.isPending) setOpen(false);
        }}
        onConfirm={() => {
          if (create.isPending) return;
          if (retryConfirmation && create.variables) {
            create.mutate(create.variables);
          } else if (
            !preview.isFetching &&
            !preview.isError &&
            preview.data &&
            selected
          ) {
            create.mutate({
              name: name.trim(),
              images: files,
              sourceNote,
              recipeId,
              expectedRecipeVersion: preview.data.recipe_version,
              previewDigest: preview.data.preview_digest,
              idempotencyKey: key.current,
            });
          }
        }}
      />
    </form>
  );
}

function ReferenceThumbnail({ file }: { file: File }) {
  const [url, setUrl] = useState("");
  useEffect(() => {
    const next = URL.createObjectURL(file);
    setUrl(next);
    return () => URL.revokeObjectURL(next);
  }, [file]);
  return url ? (
    <img
      src={url}
      alt={file.name}
      className="h-12 w-12 shrink-0 rounded object-contain"
    />
  ) : (
    <span className="h-12 w-12 shrink-0" />
  );
}
