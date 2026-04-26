interface TextAreaProps {
  label: string;
  value: string;
  onChange: (value: string) => void;
}

export function TextArea({ label, value, onChange }: TextAreaProps) {
  return (
    <label className="block">
      <span className="mb-1.5 block text-[10px] font-semibold uppercase tracking-widest text-zinc-400">
        {label}
      </span>
      <textarea
        value={value}
        onChange={(event) => onChange(event.target.value)}
        rows={5}
        className="w-full resize-none rounded-md border border-zinc-200 px-3 py-2 text-xs leading-relaxed outline-none focus:border-zinc-900 focus:ring-1 focus:ring-zinc-900"
      />
    </label>
  );
}
