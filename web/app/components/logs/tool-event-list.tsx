"use client";

import { useState } from "react";
import { cn } from "@/lib/utils";
import { formatBytes, formatDurationMs } from "@/lib/format";
import {
  ToolEventEntry,
  truncateForPreview,
} from "@/lib/tool-events";

// SummaryBlock renders a scrubbed text payload with a collapsed preview and a
// "Show more"/"Show less" toggle when it exceeds the preview length. Nothing
// here un-scrubs content — it only reshapes what the daemon already sent.
function SummaryBlock({ text, label }: { text: string; label?: string }) {
  const { preview, needsExpansion } = truncateForPreview(text);
  const [expanded, setExpanded] = useState(false);

  if (!text) return null;
  const shown = expanded ? text : preview;

  return (
    <div className="mt-1 pl-2 border-l border-border">
      {label && <span className="text-[10px] text-text-dim uppercase tracking-wider mr-1">{label}:</span>}
      <pre className="inline whitespace-pre-wrap break-words font-mono text-[11px] text-text-dim">
        {shown}
      </pre>
      {needsExpansion && (
        <button
          type="button"
          onClick={() => setExpanded((e) => !e)}
          aria-expanded={expanded}
          className="block mt-1 text-[10px] text-blue hover:text-text transition-colors"
        >
          {expanded ? "Show less" : "Show more"}
        </button>
      )}
    </div>
  );
}

// ToolEventItem renders a single tool activity entry: the tool name, and for
// results the exit code, duration, byte counts, and collapsible output summary.
export function ToolEventItem({ entry }: { entry: ToolEventEntry }) {
  if (entry.kind === "call") {
    return (
      <div data-testid="tool-call" className="py-1.5 px-2 rounded hover:bg-white/5 transition-colors">
        <div className="flex items-center gap-2">
          <span className="text-[9px] font-bold text-blue uppercase tracking-widest border border-blue/30 rounded px-1 py-0.5 shrink-0">
            Call
          </span>
          <span className="text-xs font-semibold text-text font-mono">
            {entry.call.tool_name || "tool"}
          </span>
        </div>
        <SummaryBlock text={entry.call.arguments_summary} label="args" />
      </div>
    );
  }

  const { result, call } = entry;
  const ok = result.exit_code === 0;

  return (
    <div data-testid="tool-result" className="py-1.5 px-2 rounded hover:bg-white/5 transition-colors">
      <div className="flex items-center gap-2 flex-wrap">
        <span
          className={cn(
            "text-[9px] font-bold uppercase tracking-widest border rounded px-1 py-0.5 shrink-0",
            ok
              ? "text-accent border-accent/30"
              : "text-error border-error/30"
          )}
        >
          {ok ? "Done" : "Error"}
        </span>
        <span className="text-xs font-semibold text-text font-mono">
          {result.tool_name || "tool"}
        </span>
        <span className="text-[10px] text-text-dim font-mono">exit {result.exit_code}</span>
        <span className="text-[10px] text-text-dim font-mono">{formatDurationMs(result.duration_ms)}</span>
        {result.stdout_bytes > 0 && (
          <span className="text-[10px] text-text-dim font-mono">stdout {formatBytes(result.stdout_bytes)}</span>
        )}
        {result.stderr_bytes > 0 && (
          <span className="text-[10px] text-warning font-mono">stderr {formatBytes(result.stderr_bytes)}</span>
        )}
      </div>
      {call?.arguments_summary && <SummaryBlock text={call.arguments_summary} label="args" />}
      <SummaryBlock text={result.output_summary} label="output" />
    </div>
  );
}

// ToolEventList renders the ordered tool activity stream.
export function ToolEventList({
  entries,
  emptyText = "No tool activity yet.",
}: {
  entries: ToolEventEntry[];
  emptyText?: string;
}) {
  if (entries.length === 0) {
    return <p className="text-[11px] text-text-dim italic">{emptyText}</p>;
  }

  return (
    <div data-testid="tool-event-list" className="space-y-1">
      {entries.map((entry, i) => (
        <ToolEventItem
          key={
            entry.kind === "call"
              ? `call-${entry.call.call_id}-${i}`
              : `result-${entry.result.call_id}-${i}`
          }
          entry={entry}
        />
      ))}
    </div>
  );
}
