interface ImageRatioFrameProps {
  aspectRatio: string;
  label?: string;
  className?: string;
}

export interface ParsedAspectRatio {
  width: number;
  height: number;
}

const ASPECT_RATIO_PATTERN = /^([1-9][0-9]{0,2}):([1-9][0-9]{0,2})$/;
const FRAME_MAX_WIDTH = 40;
const FRAME_MAX_HEIGHT = 40;
const FRAME_MIN_EDGE = 18;

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

export function aspectRatioFrameSize(aspectRatio: string): { width: number; height: number } {
  const parsed = parseAspectRatio(aspectRatio) ?? { width: 1, height: 1 };
  const ratio = parsed.width / parsed.height;
  if (ratio >= 1) {
    return {
      width: FRAME_MAX_WIDTH,
      height: Math.max(FRAME_MIN_EDGE, Math.round(FRAME_MAX_WIDTH / ratio)),
    };
  }
  return {
    width: Math.max(FRAME_MIN_EDGE, Math.round(FRAME_MAX_HEIGHT * ratio)),
    height: FRAME_MAX_HEIGHT,
  };
}

export function ImageRatioFrame({ aspectRatio, label, className = "" }: ImageRatioFrameProps) {
  const size = aspectRatioFrameSize(aspectRatio);
  return (
    <span className={`flex h-10 w-10 items-center justify-center ${className}`} aria-hidden="true">
      <span
        className="flex items-center justify-center rounded-sm border-2 border-current text-[10px] font-black leading-none"
        style={size}
      >
        {label}
      </span>
    </span>
  );
}
