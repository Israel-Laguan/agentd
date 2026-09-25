"use client";

import { Activity, AlertTriangle, ArrowUpRight } from "lucide-react";
import { ChatStatusReport } from "@/lib/types";

const STATE_ORDER = [
  "RUNNING",
  "READY",
  "QUEUED",
  "PENDING",
  "IN_CONSIDERATION",
  "BLOCKED",
  "NEEDS_CONTEXT",
  "COMPLETED",
  "FAILED",
  "FAILED_REQUIRES_HUMAN",
];

interface ChatStatusReportViewProps {
  report: ChatStatusReport;
  onOpenTask?: (taskId: string) => void;
}

export function ChatStatusReportView({ report, onOpenTask }: ChatStatusReportViewProps) {
  const entries = Object.entries(report.tasksByState).filter(([, n]) => n > 0);
  const sorted = entries.sort(
    (a, b) =>
      (STATE_ORDER.indexOf(a[0]) === -1 ? 99 : STATE_ORDER.indexOf(a[0])) -
      (STATE_ORDER.indexOf(b[0]) === -1 ? 99 : STATE_ORDER.indexOf(b[0]))
  );

  return (
    <div className="ml-10 p-4 bg-panel border border-border rounded-xl">
      <div className="flex items-center gap-2 mb-2">
        <Activity size={14} className="text-accent" />
        <span className="text-[11px] font-bold uppercase tracking-wider text-text">
          System status
        </span>
      </div>
      <p className="text-sm text-text mb-3 leading-relaxed">{report.message}</p>
      <div className="flex flex-wrap gap-2 text-[10px]">
        <span className="px-2 py-1 rounded bg-bg border border-border text-text-dim">
          {report.totalProjects} project{report.totalProjects === 1 ? "" : "s"}
        </span>
        {sorted.map(([state, count]) => (
          <span
            key={state}
            className="px-2 py-1 rounded bg-bg border border-border font-mono text-text-dim"
          >
            {count} {state}
          </span>
        ))}
      </div>
      {report.attention.length > 0 && (
        <div className="mt-3 space-y-2 border-t border-border pt-3">
          {report.attention.map((item) => (
            <div key={item.taskId} className="rounded border border-warning/20 bg-warning/5 p-3">
              <div className="flex items-start gap-2">
                <AlertTriangle size={14} className="mt-0.5 shrink-0 text-warning" />
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <h3 className="text-xs font-bold text-text">{item.taskTitle || item.taskId}</h3>
                    <span className="font-mono text-[9px] text-warning">{item.state}</span>
                    <span className="font-mono text-[9px] text-text-dim">{item.assignee}</span>
                  </div>
                  <p className="mt-1 text-[11px] text-text">{item.requiredAction}</p>
                  <p className="mt-1 text-[10px] leading-relaxed text-text-dim">{item.explanation}</p>
                  <p className="mt-1 text-[9px] text-text-dim">{item.projectName || item.projectId}</p>
                </div>
                {onOpenTask && (
                  <button
                    type="button"
                    onClick={() => onOpenTask(item.taskId)}
                    className="inline-flex shrink-0 items-center gap-1 rounded border border-border px-2 py-1 text-[10px] text-text hover:border-blue hover:text-blue"
                  >
                    Open in Board
                    <ArrowUpRight size={11} />
                  </button>
                )}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
