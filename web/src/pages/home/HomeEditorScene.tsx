import { useState } from "react";
import { useHomeTour } from "./HomeTourContext";
import { useI18n } from "../../lib/preferences";
import { LocalImageEditDialog } from "../workbench/local-edit/LocalImageEditDialog";

export default function HomeEditorScene({ onClose }: { onClose: () => void }) {
  const { t } = useI18n();
  const { complete } = useHomeTour();
  const [checked, setChecked] = useState(false);
  const labels = {
    title: t("localEdit.title"),
    editState: t("localEdit.editState"),
    resultState: t("localEdit.resultState"),
    source: t("localEdit.source"),
    result: t("localEdit.result"),
    compare: t("localEdit.compare"),
    operation: t("localEdit.operation"),
    instruction: t("localEdit.instruction"),
    sourceText: t("localEdit.sourceText"),
    replacementText: t("localEdit.replacementText"),
    brushSize: t("localEdit.brushSize"),
    hardness: t("localEdit.hardness"),
    eraseSelection: t("localEdit.eraseSelection"),
    clear: t("localEdit.clear"),
    submit: t("localEdit.submit"),
    close: t("localEdit.close"),
    sourceLoading: t("localEdit.sourceLoading"),
    sourceLoadFailed: t("localEdit.sourceLoadFailed"),
    actionFailed: t("localEdit.actionFailed"),
    capabilityUnavailable: t("localEdit.capabilityUnavailable"),
    targetNodeImpact: t("localEdit.targetNodeImpact"),
    resultReady: t("localEdit.resultReady"),
    keepInLibrary: t("localEdit.keepInLibrary"),
    adopt: t("localEdit.adopt"),
    revertAdoption: t("localEdit.revertAdoption"),
    continueEdit: t("localEdit.continueEdit"),
    maskCanvasLabel: t("localEdit.maskCanvasLabel"),
    sourceAlt: t("localEdit.sourceAlt"),
    resultAlt: t("localEdit.resultAlt"),
    operations: {
      remove: t("localEdit.operation.remove"),
      replace_text: t("localEdit.operation.replaceText"),
      inpaint: t("localEdit.operation.inpaint"),
    },
    validationMessages: {
      unsupported_operation: t("localEdit.validation.unsupportedOperation"),
      source_unavailable: t("localEdit.validation.sourceUnavailable"),
      invalid_source_dimensions: t("localEdit.validation.invalidSourceDimensions"),
      instruction_required: t("localEdit.validation.instructionRequired"),
      source_text_required: t("localEdit.validation.sourceTextRequired"),
      replacement_text_required: t("localEdit.validation.replacementTextRequired"),
      mask_required: t("localEdit.validation.maskRequired"),
      mask_size_mismatch: t("localEdit.validation.maskSizeMismatch"),
      mask_needs_edit_and_protection: t("localEdit.validation.maskNeedsEditAndProtection"),
    },
    statusLabels: {
      draft: t("localEdit.status.draft"),
      queued: t("localEdit.status.queued"),
      running: t("localEdit.status.running"),
      succeeded: t("localEdit.status.succeeded"),
      failed: t("localEdit.status.failed"),
      cancelled: t("localEdit.status.cancelled"),
      unknown: t("localEdit.status.unknown"),
    },
  };
  return <LocalImageEditDialog
    sourceAsset={{ url: "/home-showcase/forma-scene.webp", sourceWidth: 1134, sourceHeight: 1387, alt: t("home.shot.scene") }}
    operation="inpaint" instruction={t("home.story.backgroundRequest")}
    labels={{ ...labels, title: t("home.scene.editDemo"), submit: t("home.scene.previewMask"), targetNodeImpact: t("home.scene.editDemo") }}
    capability={{ supported: true, operations: ["remove", "replace_text", "inpaint"] }}
    targetNodeImpact={checked ? t("home.scene.maskSaved") : t("home.story.editLimit")}
    onSubmit={() => { setChecked(true); complete(2); }} onClose={onClose} onContinue={() => setChecked(false)} onKeepInLibrary={onClose}
  />;
}
