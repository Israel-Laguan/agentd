"use client";

import { useMemo, useState } from "react";
import { Check, Copy } from "lucide-react";
import { Task } from "@/lib/types";

interface HumanResolutionFormProps {
  task: Task;
  onResolve: (taskId: string, result: string) => Promise<void>;
}

function decodeCommand(value: string) {
  try {
    return JSON.parse(`"${value}"`) as string;
  } catch {
    return value.replace(/\\"/g, '"');
  }
}

export function extractBlockedCommand(description: string) {
  const encoded = description.match(/\bcommand="((?:\\.|[^"])*)"/);
  if (encoded) return decodeCommand(encoded[1]);
  const json = description.match(/"command"\s*:\s*"((?:\\.|[^"])*)"/);
  if (json) return decodeCommand(json[1]);
  const labeled = description.match(/(?:^|\n)Command:\s*([^\n]+)/i);
  if (labeled) return labeled[1].trim();
  return description.trim();
}

export function HumanResolutionForm({ task, onResolve }: HumanResolutionFormProps) {
  const [result, setResult] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [copied, setCopied] = useState(false);
  const command = useMemo(() => extractBlockedCommand(task.description), [task.description]);

  const copyCommand = async () => {
    try {
      await navigator.clipboard.writeText(command);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      setCopied(false);
    }
  };

  const resolve = async () => {
    const trimmed = result.trim();
    if (!trimmed || loading) return;
    setLoading(true);
    setError("");
    try {
      await onResolve(task.id, trimmed);
      setResult("");
    } catch {
      setError("The handoff could not be resolved. Refresh the task and try again.");
    } finally {
      setLoading(false);
    }
  };

  return (
    <section className="pt-4 border-t border-border space-y-3">
      <div>
        <h3 className="text-xs font-bold text-text">Human resolution</h3>
        <p className="text-[11px] text-text-dim mt-1">
          Run the required command in your own terminal. Agentd will not execute it.
        </p>
      </div>
      <div className="rounded border border-border bg-bg overflow-hidden">
        <div className="flex justify-between items-center px-2 py-1 border-b border-border">
          <span className="text-[9px] font-bold uppercase tracking-wider text-text-dim">Required command</span>
          <button
            type="button"
            onClick={() => void copyCommand()}
            aria-label="Copy required command"
            className="text-text-dim hover:text-text inline-flex items-center gap-1 text-[10px]"
          >
            {copied ? <Check size={12} /> : <Copy size={12} />}
            {copied ? "Copied" : "Copy"}
          </button>
        </div>
        <pre className="p-2 text-[11px] text-text font-mono whitespace-pre-wrap break-all">
          <code>{command}</code>
        </pre>
      </div>
      <label className="block text-[10px] text-text-dim" htmlFor="human-resolution-result">
        Command output or result
      </label>
      <textarea
        id="human-resolution-result"
        value={result}
        onChange={(event) => setResult(event.target.value)}
        placeholder="Paste the command output here"
        rows={5}
        className="w-full resize-y rounded border border-border bg-bg px-3 py-2 text-sm text-text focus:outline-none focus:border-blue"
      />
      {error && <p className="text-[11px] text-error">{error}</p>}
      <button
        type="button"
        onClick={() => void resolve()}
        disabled={!result.trim() || loading}
        className="w-full rounded bg-blue-600 px-3 py-2 text-sm text-white disabled:opacity-50"
      >
        {loading ? "Resolving..." : "Resolve handoff"}
      </button>
    </section>
  );
}
