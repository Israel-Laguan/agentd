"use client";

import { useRef } from "react";
import { DragEndEvent } from "@dnd-kit/core";
import { Task, TaskStatus } from "@/lib/types";
import { updateTask } from "@/lib/api";

export function useTaskBoard(
  localTasks: Task[],
  setLocalTasks: React.Dispatch<React.SetStateAction<Task[]>>,
  setSelectedTask: React.Dispatch<React.SetStateAction<Task | null>>
) {
  const dragVersionRef = useRef<Map<string, number>>(new Map());

  const handleDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;

    if (!over) return;

    const taskId = active.id as string;

    const isValidStatus = Object.values(TaskStatus).includes(
      over.id as TaskStatus
    );

    if (!isValidStatus) return;

    const newStatus = over.id as TaskStatus;

    const currentTask = localTasks.find((t) => t.id === taskId);
    const prevStatus = currentTask?.state;
    const prevUpdatedAt = currentTask?.updated_at;
    const dragVersion = (dragVersionRef.current.get(taskId) ?? 0) + 1;
    dragVersionRef.current.set(taskId, dragVersion);
    const now = Date.now();

    setLocalTasks((tasks) =>
      tasks.map((task) =>
        task.id === taskId
          ? {
              ...task,
              state: newStatus,
              updated_at: now,
            }
          : task
      )
    );
    setSelectedTask((prev) =>
      prev?.id === taskId
        ? { ...prev, state: newStatus, updated_at: now }
        : prev
    );

    updateTask(taskId, { state: newStatus, updated_at: now }).catch((err) => {
      console.error("Failed to persist task status", err);
      if (dragVersionRef.current.get(taskId) !== dragVersion) return;
      if (prevStatus === undefined || prevUpdatedAt === undefined) return;

      setLocalTasks((tasks) =>
        tasks.map((task) =>
          task.id === taskId && task.updated_at === now
            ? {
                ...task,
                state: prevStatus,
                updated_at: prevUpdatedAt,
              }
            : task
        )
      );
      setSelectedTask((prev) =>
        prev?.id === taskId && prev.updated_at === now
          ? { ...prev, state: prevStatus, updated_at: prevUpdatedAt }
          : prev
      );
    });
  };

  const handleUpdateTask = async (id: string, patch: Partial<Task>) => {
    const updated = await updateTask(id, patch);
    setLocalTasks((prev) => prev.map((t) => (t.id === id ? updated : t)));
    setSelectedTask((prev) => (prev?.id === id ? updated : prev));
  };

  return { handleDragEnd, handleUpdateTask };
}
