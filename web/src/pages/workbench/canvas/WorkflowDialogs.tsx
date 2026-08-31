import { useEffect, useId, useState } from "react";

import { Button } from "../../../components/ui/button";
import { Dialog, DialogContent } from "../../../components/ui/dialog";
import { Input, TextArea } from "../../../components/ui/field";
import { useI18n } from "../../../lib/preferences";

interface WorkflowTextDialogProps {
  open: boolean;
  title: string;
  label: string;
  initialValue?: string;
  maxLength?: number;
  busy?: boolean;
  error?: string | null;
  onClose: () => void;
  onSubmit: (value: string) => void;
}

export function WorkflowTextDialog({
  open,
  title,
  label,
  initialValue = "",
  maxLength = 120,
  busy = false,
  error,
  onClose,
  onSubmit,
}: WorkflowTextDialogProps) {
  const { t } = useI18n();
  const inputId = useId();
  const [value, setValue] = useState(initialValue);

  useEffect(() => {
    if (open) {
      setValue(initialValue);
    }
  }, [initialValue, open]);

  const normalized = value.trim();
  return (
    <Dialog open={open} onOpenChange={(next) => { if (!next && !busy) onClose(); }}>
      <DialogContent
        title={title}
        closeLabel={t("workbench.dialog.close")}
        footer={(
          <>
            <Button variant="secondary" onClick={onClose} disabled={busy}>{t("common.cancel")}</Button>
            <Button
              variant="primary"
              disabled={!normalized}
              busy={busy}
              onClick={() => { if (normalized) onSubmit(normalized); }}
            >
              {t("workbench.dialog.confirm")}
            </Button>
          </>
        )}
      >
        <form
          onSubmit={(event) => {
            event.preventDefault();
            if (normalized) onSubmit(normalized);
          }}
        >
          <Input
            id={inputId}
            label={label}
            value={value}
            onChange={(event) => setValue(event.target.value)}
            maxLength={maxLength}
            autoFocus
            error={error}
          />
        </form>
      </DialogContent>
    </Dialog>
  );
}

interface WorkflowReferenceNodeDialogProps {
  open: boolean;
  busy?: boolean;
  error?: string | null;
  onClose: () => void;
  onSubmit: (input: { title: string; role: string; label: string }) => void;
}

export function WorkflowReferenceNodeDialog({
  open,
  busy = false,
  error,
  onClose,
  onSubmit,
}: WorkflowReferenceNodeDialogProps) {
  const { t } = useI18n();
  const [title, setTitle] = useState("");
  const [role, setRole] = useState("");
  const [label, setLabel] = useState("");

  useEffect(() => {
    if (open) {
      setTitle(t("workbench.reference.defaultTitle"));
      setRole(t("workbench.reference.defaultRole"));
      setLabel(t("workbench.reference.defaultLabel"));
    }
  }, [open, t]);

  const normalized = { title: title.trim(), role: role.trim(), label: label.trim() };
  const valid = Boolean(normalized.title && normalized.role && normalized.label);

  return (
    <Dialog open={open} onOpenChange={(next) => { if (!next && !busy) onClose(); }}>
      <DialogContent
        title={t("workbench.reference.create")}
        closeLabel={t("workbench.dialog.close")}
        footer={(
          <>
            <Button variant="secondary" onClick={onClose} disabled={busy}>{t("common.cancel")}</Button>
            <Button
              variant="primary"
              disabled={!valid}
              busy={busy}
              onClick={() => { if (valid) onSubmit(normalized); }}
            >
              {t("workbench.dialog.confirm")}
            </Button>
          </>
        )}
      >
        <div className="space-y-4">
          <Input label={t("workbench.reference.titleField")} value={title} maxLength={255} autoFocus onChange={(event) => setTitle(event.target.value)} />
          <Input label={t("workbench.reference.roleField")} value={role} maxLength={120} onChange={(event) => setRole(event.target.value)} />
          <Input label={t("workbench.reference.labelField")} value={label} maxLength={255} onChange={(event) => setLabel(event.target.value)} />
          {error ? <p role="alert" className="text-xs text-state-error">{error}</p> : null}
        </div>
      </DialogContent>
    </Dialog>
  );
}

interface WorkflowRecipeDialogProps {
  open: boolean;
  heading: string;
  sourceLabel: string;
  initialTitle?: string;
  initialDescription?: string | null;
  busy?: boolean;
  error?: string | null;
  onClose: () => void;
  onSubmit: (title: string, description: string | null) => void;
}

export function WorkflowRecipeDialog({
  open,
  heading,
  sourceLabel,
  initialTitle = "",
  initialDescription = null,
  busy = false,
  error,
  onClose,
  onSubmit,
}: WorkflowRecipeDialogProps) {
  const { t } = useI18n();
  const [title, setTitle] = useState(initialTitle);
  const [description, setDescription] = useState(initialDescription ?? "");

  useEffect(() => {
    if (open) {
      setTitle(initialTitle);
      setDescription(initialDescription ?? "");
    }
  }, [initialDescription, initialTitle, open]);

  const normalizedTitle = title.trim();
  return (
    <Dialog open={open} onOpenChange={(next) => { if (!next && !busy) onClose(); }}>
      <DialogContent
        title={heading}
        description={sourceLabel}
        size="lg"
        closeLabel={t("workbench.dialog.close")}
        footer={(
          <>
            <Button variant="secondary" onClick={onClose} disabled={busy}>{t("common.cancel")}</Button>
            <Button
              variant="primary"
              disabled={!normalizedTitle}
              busy={busy}
              onClick={() => { if (normalizedTitle) onSubmit(normalizedTitle, description.trim() || null); }}
            >
              {t("workbench.recipe.save")}
            </Button>
          </>
        )}
      >
        <div className="space-y-4">
          <Input label={t("workbench.recipe.titleField")} value={title} maxLength={255} autoFocus onChange={(event) => setTitle(event.target.value)} />
          <TextArea label={t("workbench.recipe.descriptionField")} value={description} maxLength={4000} minRows={4} onChange={setDescription} />
          {error ? <p role="alert" className="text-xs text-state-error">{error}</p> : null}
        </div>
      </DialogContent>
    </Dialog>
  );
}
