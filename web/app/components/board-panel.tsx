"use client";

import React from "react";
import { motion } from "framer-motion";
import { cn } from '@/lib/utils';
import { TaskStatus, Task, Project } from '@/lib/types';
import { TaskCard } from "@/app/components/task-card";

interface BoardPanelProps {
  boardData: { projects: Project[], tasks: Task[] };
}

export function BoardPanel({ boardData }: BoardPanelProps) {
  const tasksByStatus = React.useMemo(() => {
    const groups: Record<TaskStatus, Task[]> = {
      [TaskStatus.PENDING]: [],
      [TaskStatus.READY]: [],
      [TaskStatus.QUEUED]: [],
      [TaskStatus.RUNNING]: [],
      [TaskStatus.COMPLETED]: [],
      [TaskStatus.FAILED]: [],
      [TaskStatus.BLOCKED]: [],
      [TaskStatus.IN_CONSIDERATION]: []
    };
    for (const task of boardData.tasks) {
      if (groups[task.status]) {
        groups[task.status].push(task);
      }
    }
    for (const status in groups) {
      groups[status as TaskStatus].sort((a, b) => b.updatedAt - a.updatedAt);
    }
    return groups;
  }, [boardData.tasks]);

  return (
    <motion.div
      key="board"
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      className="h-full flex flex-col"
    >
      <div className="flex items-center justify-between mb-8">
        <div>
          <h2 className="text-xl font-bold text-text uppercase tracking-tight">Active Operation Board</h2>
          <p className="text-[11px] text-text-dim font-mono mt-1">REAL-TIME WORKFORCE DISPATCH SYSTEM</p>
        </div>
        <div className="flex items-center gap-2">
          <div className="flex -space-x-2">
            {[1,2,3].map(i => <div key={i} className="w-6 h-6 rounded-full bg-border border-2 border-panel flex items-center justify-center text-[8px] font-bold">W{i}</div>)}
          </div>
          <span className="ml-2 px-2 py-0.5 bg-blue/10 text-blue text-[9px] font-bold rounded border border-blue/20 uppercase tracking-widest">Queue Healthy</span>
        </div>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-8 gap-4 h-full content-start overflow-x-auto min-w-full pb-10">
        {[
          TaskStatus.PENDING,
          TaskStatus.READY,
          TaskStatus.QUEUED,
          TaskStatus.RUNNING,
          TaskStatus.BLOCKED,
          TaskStatus.IN_CONSIDERATION,
          TaskStatus.COMPLETED,
          TaskStatus.FAILED,
        ].map(status => (
          <div key={status} className="flex flex-col gap-3 min-w-[240px]">
            <div className="flex justify-between items-center px-1 py-1.5 border-b border-border shadow-[0_1px_0_var(--color-bg)]">
              <h3 className={cn(
                "text-[10px] font-bold uppercase tracking-[0.2em]",
                status === TaskStatus.RUNNING ? "text-blue" :
                status === TaskStatus.READY ? "text-green-400" : "text-text-dim"
              )}>{status}</h3>
              <span className="text-[9px] font-mono bg-border/40 px-1.5 rounded text-text-dim">{tasksByStatus[status].length}</span>
            </div>
            <div className="space-y-3 px-0.5">
              {tasksByStatus[status].map(task => (
                <TaskCard key={task.id} task={task} />
              ))}
              {tasksByStatus[status].length === 0 && (
                <div className="h-24 border border-dashed border-border/50 rounded-md flex items-center justify-center bg-bg/20">
                  <span className="text-[9px] font-bold text-text-dim/30 uppercase tracking-widest">Idle</span>
                </div>
              )}
            </div>
          </div>
        ))}
      </div>
    </motion.div>
  );
}
