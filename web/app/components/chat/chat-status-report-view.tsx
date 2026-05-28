"use client";

import { Activity } from "lucide-react";
import { ChatStatusReport } from "@/lib/types";

const STATE_ORDER = [
  "RUNNING",
  "READY",
  "QUEUED",
  "PENDING",
  "IN_CONSIDERATION",
  "BLOCKED",
  "COMPLETED",
  "FAILED",
  "FAILED_REQUIRES_HUMAN",
];

export function ChatStatusReportView({ report }: { report: ChatStatusReport }) {
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
    </div>
  );
}
