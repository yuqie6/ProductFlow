// A small camera character: the lens looks toward the work and the shutter
// changes with the selected stage. It is decorative, never a status indicator.
export function StudioCamera({ step }: { step: number }) {
  return (
    <svg className="studio-camera" data-step={step} viewBox="0 0 180 164" fill="none" aria-hidden="true">
      <ellipse className="studio-camera-shadow" cx="92" cy="147" rx="49" ry="7" fill="currentColor" opacity=".08" />
      <g className="studio-camera-body">
        <path d="M32 66Q22 71 19 90M148 66Q160 72 162 90" stroke="currentColor" strokeWidth="5" strokeLinecap="round" />
        <path d="m58 128-7 12m70-12 7 12" stroke="currentColor" strokeWidth="6" strokeLinecap="round" />
        <rect x="62" y="18" width="55" height="20" rx="9" fill="var(--color-surface-subtle)" stroke="currentColor" strokeWidth="2" />
        <rect x="29" y="32" width="123" height="99" rx="30" fill="var(--color-surface-subtle)" stroke="currentColor" strokeWidth="2" />
        <rect x="29" y="27" width="116" height="97" rx="29" fill="var(--color-surface-raised)" stroke="currentColor" strokeWidth="2" />
        <rect x="46" y="43" width="18" height="7" rx="3.5" fill="currentColor" opacity=".16" />
        <circle cx="126" cy="45" r="4" fill="var(--color-accent)" />
        <circle cx="87" cy="79" r="31" fill="var(--color-surface-subtle)" stroke="currentColor" strokeWidth="2" />
        <g className="studio-camera-lens">
          <circle cx="87" cy="79" r="23" fill="currentColor" />
          <ellipse cx="88" cy="77" rx="10" ry="13" fill="var(--color-surface-raised)" />
          <circle cx="94" cy="70" r="4" fill="var(--color-surface-raised)" />
        </g>
        <path d="M80 116q7 5 14 0" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
      </g>
    </svg>
  );
}
