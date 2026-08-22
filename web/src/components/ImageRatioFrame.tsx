interface ImageRatioFrameProps {
  aspectRatio: string;
  label?: string;
  className?: string;
  size?: "sm" | "md";
}

export interface ParsedAspectRatio {
  width: number;
  height: number;
}

const ASPECT_RATIO_PATTERN = /^([1-9][0-9]{0,2}):([1-9][0-9]{0,2})$/;
const FRAME_SIZE = {
  md: { maxWidth: 40, maxHeight: 40, minEdge: 18, wrap: "h-10 w-10" },
  sm: { maxWidth: 20, maxHeight: 20, minEdge: 9, wrap: "h-5 w-5" },
} as const;

export function parseAspectRatio(value: string): ParsedAspectRatio | null {
  const match = ASPECT_RATIO_PATTERN.exec(value.trim());
  if (!match) {
    return null;
  }
  return { width: Number(match[1]), height: Number(match[2]) };
}

export function formatAspectRatio(width: string, height: string): string | null {
  if (!/^\d{1,3}$/.test(width) || !/^\d{1,3}$/.test(height)) {
    return null;
  }
  const nextWidth = Number(width);
  const nextHeight = Number(height);
  if (nextWidth < 1 || nextHeight < 1) {
    return null;
  }
  return `${nextWidth}:${nextHeight}`;
}

export function aspectRatioFrameSize(
  aspectRatio: string,
  size: keyof typeof FRAME_SIZE = "md",
): { width: number; height: number } {
  const { maxWidth, maxHeight, minEdge } = FRAME_SIZE[size];
  const parsed = parseAspectRatio(aspectRatio) ?? { width: 1, height: 1 };
  const ratio = parsed.width / parsed.height;
  if (ratio >= 1) {
    return {
      width: maxWidth,
      height: Math.max(minEdge, Math.round(maxWidth / ratio)),
    };
  }
  return {
    width: Math.max(minEdge, Math.round(maxHeight * ratio)),
    height: maxHeight,
  };
}

export function ImageRatioFrame({
  aspectRatio,
  label,
  className = "",
  size = "md",
}: ImageRatioFrameProps) {
  const frame = aspectRatioFrameSize(aspectRatio, size);
  return (
    <span className={`flex items-center justify-center ${FRAME_SIZE[size].wrap} ${className}`} aria-hidden="true">
      <span
        className="flex items-center justify-center rounded-sm border-2 border-current text-[10px] font-black leading-none"
        style={frame}
      >
        {label}
      </span>
    </span>
  );
}
