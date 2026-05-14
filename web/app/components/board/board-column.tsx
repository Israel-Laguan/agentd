"use client";

import { useDroppable } from "@dnd-kit/core";

import {
  cn,
  Task,
  TaskStatus,
} from "@/lib/types";

import { TaskCard } from "../task/task-card";

interface BoardColumnProps {
  status: TaskStatus;
  tasks: Task[];
  onTaskClick: (task: Task) => void;
}

export function BoardColumn({
  status,
  tasks,
  onTaskClick,
}: BoardColumnProps) {
  const { setNodeRef, isOver } = useDroppable({
    id: status,
  });

  return (
    <div
      ref={setNodeRef}
      className={cn(
        "flex flex-col gap-3 min-w-[240px] rounded-lg transition-colors",
        isOver && "bg-blue/5"
      )}
    >
      <div className="flex justify-between items-center px-1 py-1.5 border-b border-border shadow-[0_1px_0_var(--color-bg)]">
        <h3
          className={cn(
            "text-[10px] font-bold uppercase tracking-[0.2em]",
            status === TaskStatus.RUNNING
              ? "text-blue"
              : "text-text-dim"
          )}
        >
          {status}
        </h3>

        <span className="text-[9px] font-mono bg-border/40 px-1.5 rounded text-text-dim">
          {tasks.length}
        </span>
      </div>

      <div className="space-y-3 px-0.5 min-h-[200px]">
        {tasks.map(task => (
          <TaskCard
            key={task.id}
            task={task}
            onClick={() => onTaskClick(task)}
          />
        ))}

        {tasks.length === 0 && (
          <div className="h-24 border border-dashed border-border/50 rounded-md flex items-center justify-center bg-bg/20">
            <span className="text-[9px] font-bold text-text-dim/30 uppercase tracking-widest">
              Idle
            </span>
          </div>
        )}
      </div>
    </div>
  );
}