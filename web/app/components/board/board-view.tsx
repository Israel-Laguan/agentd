"use client";

import { motion } from "framer-motion";
import {
  DndContext,
  DragEndEvent,
} from "@dnd-kit/core";

import {
  Task,
  TaskStatus,
} from "@/lib/types";

import { BoardColumn } from "./board-column";

interface BoardViewProps {
  tasks: Task[];
  onDragEnd: (event: DragEndEvent) => void;
  onTaskClick: (task: Task) => void;
}

export function BoardView({
  tasks,
  onDragEnd,
  onTaskClick,
}: BoardViewProps) {
  return (
    <motion.div
      key="board"
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      className="h-full flex flex-col"
    >
      <div className="flex items-center justify-between mb-8">
        <div>
          <h2 className="text-xl font-bold text-text uppercase tracking-tight">
            Active Operation Board
          </h2>

          <p className="text-[11px] text-text-dim font-mono mt-1">
            REAL-TIME WORKFORCE DISPATCH SYSTEM
          </p>
        </div>

        <div className="flex items-center gap-2">
          <div className="flex -space-x-2">
            {[1, 2, 3].map(i => (
              <div
                key={i}
                className="w-6 h-6 rounded-full bg-border border-2 border-panel flex items-center justify-center text-[8px] font-bold"
              >
                W{i}
              </div>
            ))}
          </div>

          <span className="ml-2 px-2 py-0.5 bg-blue/10 text-blue text-[9px] font-bold rounded border border-blue/20 uppercase tracking-widest">
            Queue Healthy
          </span>
        </div>
      </div>

      <DndContext onDragEnd={onDragEnd}>
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4 h-full content-start overflow-x-auto min-w-full pb-10">
          {[
            TaskStatus.PENDING,
            TaskStatus.RUNNING,
            TaskStatus.COMPLETED,
            TaskStatus.FAILED,
          ].map(status => (
            <BoardColumn
              key={status}
              status={status}
              tasks={tasks
                .filter(t => t.status === status)
                .sort((a, b) => b.updatedAt - a.updatedAt)}
              onTaskClick={onTaskClick}
            />
          ))}
        </div>
      </DndContext>
    </motion.div>
  );
}