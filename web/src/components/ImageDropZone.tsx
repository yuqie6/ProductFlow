import { useRef, useState } from "react";
import type { DragEvent, KeyboardEvent, ReactNode } from "react";

interface ImageDropZoneState {
  isDragging: boolean;
}

interface ImageDropZoneProps {
  accept?: string;
  multiple?: boolean;
  disabled?: boolean;
  className: string;
  activeClassName?: string;
  focusClassName?: string;
  inputClassName?: string;
  ariaLabel?: string;
  onFiles: (files: File[]) => void;
  children: ReactNode | ((state: ImageDropZoneState) => ReactNode);
}

function filesFromList(fileList: FileList, multiple: boolean) {
  const files = Array.from(fileList);
  return multiple ? files : files.slice(0, 1);
}

export function ImageDropZone({
  accept = "image/png,image/jpeg,image/webp",
  multiple = false,
  disabled = false,
  className,
  activeClassName = "border-zinc-900 bg-zinc-100 text-zinc-900",
  focusClassName = "focus:outline-none focus:ring-2 focus:ring-zinc-900/20",
  inputClassName = "hidden",
  ariaLabel,
  onFiles,
  children,
}: ImageDropZoneProps) {
  const dragDepth = useRef(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const [isDragging, setIsDragging] = useState(false);

  const resetDragState = () => {
    dragDepth.current = 0;
    setIsDragging(false);
  };

  const handleDragEnter = (event: DragEvent<HTMLLabelElement>) => {
    event.preventDefault();
    event.stopPropagation();
    if (disabled) {
      return;
    }
    dragDepth.current += 1;
    setIsDragging(true);
  };

  const handleDragOver = (event: DragEvent<HTMLLabelElement>) => {
    event.preventDefault();
    event.stopPropagation();
    event.dataTransfer.dropEffect = disabled ? "none" : "copy";
  };

  const handleDragLeave = (event: DragEvent<HTMLLabelElement>) => {
    event.preventDefault();
    event.stopPropagation();
    if (disabled) {
      resetDragState();
      return;
    }
    dragDepth.current = Math.max(0, dragDepth.current - 1);
    if (dragDepth.current === 0) {
      setIsDragging(false);
    }
  };

  const handleDrop = (event: DragEvent<HTMLLabelElement>) => {
    event.preventDefault();
    event.stopPropagation();
    resetDragState();
    if (disabled) {
      return;
    }
    const files = filesFromList(event.dataTransfer.files, multiple);
    if (files.length) {
      onFiles(files);
    }
  };

  const handleKeyDown = (event: KeyboardEvent<HTMLLabelElement>) => {
    if (disabled || (event.key !== "Enter" && event.key !== " ")) {
      return;
    }
    event.preventDefault();
    inputRef.current?.click();
  };

  const activeDragging = isDragging && !disabled;

  return (
    <label
      role="button"
      tabIndex={disabled ? -1 : 0}
      aria-disabled={disabled}
      aria-label={ariaLabel}
      className={`${className} ${focusClassName} ${activeDragging ? activeClassName : ""} ${
        disabled ? "cursor-not-allowed opacity-60" : ""
      }`}
      onDragEnter={handleDragEnter}
      onDragOver={handleDragOver}
      onDragLeave={handleDragLeave}
      onDrop={handleDrop}
      onKeyDown={handleKeyDown}
    >
      {typeof children === "function" ? children({ isDragging: activeDragging }) : children}
      <input
        ref={inputRef}
        type="file"
        accept={accept}
        multiple={multiple}
        disabled={disabled}
        className={inputClassName}
        onChange={(event) => {
          const selectedFiles = event.target.files;
          const files = selectedFiles ? filesFromList(selectedFiles, multiple) : [];
          if (files.length) {
            onFiles(files);
          }
          event.currentTarget.value = "";
        }}
      />
    </label>
  );
}
