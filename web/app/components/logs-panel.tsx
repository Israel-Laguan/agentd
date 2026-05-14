"use client";

import { motion } from "framer-motion";
import { cn, Task } from '@/lib/types';

interface LogsPanelProps {
  boardData: { projects: any[], tasks: Task[] };
}

export function LogsPanel({ boardData }: LogsPanelProps) {
  return (
    <motion.div
      key="logs"
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      className="h-full flex flex-col font-mono"
    >
      <div className="flex-1 bg-bg border border-border rounded-xl p-6 overflow-y-auto text-[11px] leading-relaxed shadow-inner">
        <div className="mb-4 text-text-dim/40 italic flex justify-between items-center border-b border-border pb-2">
          <span>agentd Kernel View - Task Execution Stream</span>
          <span className="animate-pulse">● System Live</span>
        </div>
        {boardData.tasks
          .flatMap(t => t.logs)
          .sort((a, b) => new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime())
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
                  log.message.startsWith('[SYSTEM]') ? "text-blue" :
                  log.message.startsWith('[AGENT]') ? "text-text" :
                  log.message.startsWith('[ERROR]') ? "text-error font-bold" :
                  "text-text-dim"
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
