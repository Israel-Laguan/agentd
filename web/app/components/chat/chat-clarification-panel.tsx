"use client";

import { ChatScopeOption } from "@/lib/types";

export function ChatScopeClarificationPanel({
  scopes,
  onSelect,
}: {
  scopes: ChatScopeOption[];
  onSelect: (scopeId: string) => void;
}) {
  return (
    <div className="ml-10 flex flex-col gap-2">
      <p className="text-sm text-text-dim">Select a scope to proceed:</p>
      <div className="flex flex-wrap gap-2">
        {scopes.map((scope) => (
          <button
            key={scope.id}
            type="button"
            onClick={() => onSelect(scope.id)}
            className="px-3 py-1.5 text-sm border border-text-dim rounded hover:border-text hover:text-text transition-all"
          >
            {scope.label || scope.id}
          </button>
        ))}
      </div>
    </div>
  );
}

export function ChatIntentClarificationPanel({
  onStatusCheck,
  onPlanWork,
}: {
  onStatusCheck: () => void;
  onPlanWork: () => void;
}) {
  return (
    <div className="ml-10 flex flex-wrap gap-2">
      <button
        type="button"
        onClick={onStatusCheck}
        className="px-3 py-1.5 text-sm border border-accent/40 text-accent rounded hover:bg-accent/10 transition-all"
      >
        Check project status
      </button>
      <button
        type="button"
        onClick={onPlanWork}
        className="px-3 py-1.5 text-sm border border-text-dim rounded hover:border-text hover:text-text transition-all"
      >
        Plan new work
      </button>
    </div>
  );
}
