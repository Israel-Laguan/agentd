"use client";

import { motion } from "framer-motion";
import { Task } from "@/lib/types";
import { cn } from "@/lib/utils";
import { ToolEventList } from "@/app/components/logs/tool-event-list";
import { useToolEventStream } from "@/app/hooks/use-tool-event-stream";

interface LogsViewProps {
  tasks: Task[];
}

export function LogsView({ tasks }: LogsViewProps) {
  const { entries, connected } = useToolEventStream();

  return (
    <motion.div
      key="logs"
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      className="h-full flex flex-col font-mono"
    >
      <div className="flex-1 bg-bg border border-border rounded-xl p-6 overflow-y-auto text-[11px] leading-relaxed shadow-inner">

        {/* HEADER (unchanged) */}
        <div className="mb-4 text-text-dim/40 italic flex justify-between items-center border-b border-border pb-2">
          <span>agentd Kernel View - Task Execution Stream</span>
          <span className="animate-pulse">● System Live</span>
        </div>

        {/* TOOL ACTIVITY — live tool_called / tool_result stream */}
        <h2
          className="mb-2 w-full flex items-center gap-2 text-[10px] font-bold uppercase tracking-widest text-text-dim/70 hover:text-text transition-colors"
        >
          <span>Tool Activity</span>
          <span
            className={cn(
              "h-1.5 w-1.5 rounded-full",
              connected ? "bg-accent" : "bg-blue"
            )}
          />
        </h2>
        <div className="mb-6">
          <ToolEventList entries={entries} />
        </div>

        {/* AGENT LOG LINES */}
        {tasks
          .flatMap((t) => t.logs)
          .sort((a, b) => a.timestamp - b.timestamp)
          .slice(-150)
          .map((log, i) => (
            <div
              key={`${log.timestamp}-${i}`}
              className="flex gap-3 hover:bg-white/5 py-0.5 px-1 rounded transition-colors group"
            >
              <span className="text-blue/40 shrink-0 select-none">
                [{new Date(log.timestamp).toLocaleTimeString()}]
              </span>

              <span
                className={cn(
                  "flex-1",
                  log.message.startsWith("[SYSTEM]")
                    ? "text-blue"
                    : log.message.startsWith("[AGENT]")
                    ? "text-text"
                    : log.message.startsWith("[ERROR]")
                    ? "text-error font-bold"
                    : "text-text-dim"
                )}
              >
                {log.message}
              </span>
            </div>
          ))}

        <div className="h-4" />
      </div>
    </motion.div>
  );
}

