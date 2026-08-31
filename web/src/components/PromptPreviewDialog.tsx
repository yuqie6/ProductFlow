import { useI18n } from "../lib/preferences";
import { Dialog, DialogContent } from "./ui/dialog";

export interface PromptPreview {
  title: string;
  text: string;
  meta?: string;
}

interface PromptPreviewDialogProps {
  preview: PromptPreview;
  onClose: () => void;
}

export function PromptPreviewDialog({ preview, onClose }: PromptPreviewDialogProps) {
  const { t } = useI18n();
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title={preview.title}
        description={preview.meta}
        size="xl"
        closeLabel={t("promptPreview.close")}
        onClose={onClose}
        bodyClassName="max-h-[60vh] overflow-y-auto"
      >
          <pre className="whitespace-pre-wrap break-words rounded-control bg-surface-subtle p-4 text-sm leading-6 text-text-primary">
            {preview.text}
          </pre>
      </DialogContent>
    </Dialog>
  );
}
