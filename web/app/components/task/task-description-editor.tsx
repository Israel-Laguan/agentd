"use client";

interface TaskDescriptionEditorProps {
  value: string;
  editing: boolean;
  onChange: (value: string) => void;
  onEditingChange: (editing: boolean) => void;
  // Called when the textarea loses focus; the owner decides whether to save.
  onCommit: () => void;
}

export function TaskDescriptionEditor({
  value,
  editing,
  onChange,
  onEditingChange,
  onCommit,
}: TaskDescriptionEditorProps) {
  return (
    <div>
      <label className="text-[10px] text-text-dim">Description</label>

      {editing ? (
        <textarea
          value={value}
          onChange={(e) => onChange(e.target.value)}
          onBlur={() => {
            onEditingChange(false);
            onCommit();
          }}
          autoFocus
          className="w-full text-sm text-text-dim bg-bg border border-border rounded px-2 py-1 min-h-[100px]"
        />
      ) : (
        <p
          className="text-sm text-text-dim cursor-text whitespace-pre-wrap focus:outline-none focus:ring-2 focus:ring-accent rounded px-1 -mx-1"
          onClick={() => onEditingChange(true)}
          tabIndex={0}
          role="button"
          aria-label="Edit description"
          onKeyDown={(e) => {
            if (e.key === "Enter" || e.key === " ") {
              e.preventDefault();
              onEditingChange(true);
            }
          }}
        >
          {value}
        </p>
      )}
    </div>
  );
}
