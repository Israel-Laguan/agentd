"use client";

import { History } from "lucide-react";
import { TaskEvent } from "@/lib/types";

export function TaskEventList({ events }: { events: TaskEvent[] }) {
  if (events.length === 0) {
    return <p className="text-[11px] text-text-dim">No task events yet.</p>;
  }

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <History size={13} className="text-text-dim" />
        <h3 className="text-xs font-bold text-text">Task events</h3>
      </div>
      {[...events].reverse().map((event) => (
        <details key={event.id} className="rounded border border-border bg-bg">
          <summary className="cursor-pointer px-2 py-2 text-[10px] text-text">
            <span className="font-mono text-blue">{event.type}</span>
            <span className="ml-2 text-text-dim">{new Date(event.created_at).toLocaleString()}</span>
          </summary>
          <pre className="max-h-48 overflow-y-auto whitespace-pre-wrap break-words border-t border-border px-2 py-2 font-mono text-[10px] leading-relaxed text-text-dim">
            {event.payload || "No payload"}
          </pre>
          {event.payload_truncated && <p className="px-2 pb-2 text-[9px] text-warning">Payload truncated</p>}
        </details>
      ))}
    </div>
  );
}
