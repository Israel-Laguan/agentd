"use client";

import { useState, useEffect, useCallback } from "react";
import { Task, WorkforceState, SystemStatus, MaterializeResult } from "@/lib/types";
import { getBoard, getWorkforce, getSystemStatus } from "@/lib/api";

export function useDashboard(
  setSelectedTask?: React.Dispatch<React.SetStateAction<Task | null>>
) {
  const [workforce, setWorkforce] = useState<WorkforceState | null>(null);
  const [localTasks, setLocalTasks] = useState<Task[]>([]);
  const [boardError, setBoardError] = useState(false);
  const [systemStatus, setSystemStatus] = useState<SystemStatus | null>(null);

  const applyDashboardSnapshot = useCallback(
    (
      board: { tasks: Task[] },
      workforce: WorkforceState | null,
      status: SystemStatus | null,
      replaceTasks: boolean
    ) => {
      setBoardError(false);
      setWorkforce(workforce);
      setSystemStatus(status);
      setLocalTasks((prev) =>
        replaceTasks
          ? board.tasks
          : prev.length === 0
            ? board.tasks
            : board.tasks.map((serverTask: Task) => {
                const localTask = prev.find((t) => t.id === serverTask.id);
                return localTask && localTask.updated_at > serverTask.updated_at
                  ? localTask
                  : serverTask;
              })
      );
      setSelectedTask?.((prev) => {
        if (!prev) return prev;
        const fresh = board.tasks.find((t: Task) => t.id === prev.id);
        return fresh && fresh.updated_at > prev.updated_at ? fresh : prev;
      });
      return board.tasks;
    },
    []
  );

  const refreshDashboard = useCallback(
    async (replaceTasks = false): Promise<Task[]> => {
      const [boardRes, workforceRes, statusRes] = await Promise.allSettled([
        getBoard(),
        getWorkforce(),
        getSystemStatus(),
      ]);
      if (boardRes.status !== "fulfilled" || workforceRes.status !== "fulfilled") {
        setBoardError(true);
        setSystemStatus(statusRes.status === "fulfilled" ? statusRes.value : null);
        throw new Error("dashboard refresh failed");
      }
      return applyDashboardSnapshot(
        boardRes.value,
        workforceRes.value,
        statusRes.status === "fulfilled" ? statusRes.value : null,
        replaceTasks
      );
    },
    [applyDashboardSnapshot]
  );

  useEffect(() => {
    let mounted = true;

    const poll = async () => {
      try {
        await refreshDashboard(false);
        if (!mounted) return;
      } catch (e) {
        if (!mounted) return;
        console.error("Polling failed", e);
        setBoardError(true);
        setSystemStatus(null);
      }
    };

    void poll();

    const interval = setInterval(() => {
      if (!mounted) return;
      void poll();
    }, 3000);

    return () => {
      mounted = false;
      clearInterval(interval);
    };
  }, [refreshDashboard]);

  const handleMaterialized = useCallback(
    async (result: MaterializeResult, onFocus: (task: Task) => void) => {
      const tasks = await refreshDashboard(true);
      const focus =
        tasks.find((t) => result.taskIds.includes(t.id)) ??
        tasks.find((t) => t.project_id === result.projectId);
      if (focus) onFocus(focus);
    },
    [refreshDashboard]
  );

  return {
    workforce,
    localTasks,
    setLocalTasks,
    boardError,
    systemStatus,
    refreshDashboard,
    handleMaterialized,
  };
}
