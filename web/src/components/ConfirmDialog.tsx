import { Button } from "./ui/button";
import { Dialog, DialogContent } from "./ui/dialog";
import type { ReactNode } from "react";

interface ConfirmDialogProps {
  open: boolean;
  title: string;
  description: string;
  body?: ReactNode;
  confirmLabel: string;
  cancelLabel: string;
  busy?: boolean;
  confirmDisabled?: boolean;
  destructive?: boolean;
  onConfirm: () => void;
  onClose: () => void;
}

export function ConfirmDialog({
  open,
  title,
  description,
  body,
  confirmLabel,
  cancelLabel,
  busy = false,
  confirmDisabled = false,
  destructive = true,
  onConfirm,
  onClose,
}: ConfirmDialogProps) {
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next && !busy) {
          onClose();
        }
      }}
    >
      <DialogContent
        title={title}
        description={description}
        hideClose
        footer={(
          <>
            <Button variant="secondary" onClick={onClose} disabled={busy}>
              {cancelLabel}
            </Button>
            <Button
              variant={destructive ? "danger" : "primary"}
              onClick={onConfirm}
              busy={busy}
              disabled={confirmDisabled}
            >
              {confirmLabel}
            </Button>
          </>
        )}
      >
        {body}
      </DialogContent>
    </Dialog>
  );
}
