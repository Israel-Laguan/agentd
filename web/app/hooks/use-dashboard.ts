"use client";

import { useState, useEffect, useCallback } from "react";
import { BoardSnapshot, Task, WorkforceState, SystemStatus, MaterializeResult } from "@/lib/types";

export type BoardIssues = Pick<BoardSnapshot, "failedProjects" | "cappedProjects">;

// DaemonState is what the header says about the daemon: connecting until the
// first refresh settles, then online or unreachable.
export type DaemonState = "connecting" | "online" | "unreachable";

const NO_ISSUES: BoardIssues = { failedProjects: [], cappedProjects: [] };
import { getBoard, getProjectTasks, getWorkforce, getSystemStatus } from "@/lib/api";

// The SSE stream (use-board-stream) drives refreshes; this poll only covers
// events it missed.
const FALLBACK_POLL_MS = 30_000;

export function useDashboard(
  setSelectedTask?: React.Dispatch<React.SetStateAction<Task | null>>
) {
  const [workforce, setWorkforce] = useState<WorkforceState | null>(null);
  const [localTasks, setLocalTasks] = useState<Task[]>([]);
  const [boardError, setBoardError] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const [boardIssues, setBoardIssues] = useState<BoardIssues>(NO_ISSUES);
  const [systemStatus, setSystemStatus] = useState<SystemStatus | null>(null);

  const applyDashboardSnapshot = useCallback(
    (
      board: BoardSnapshot,
      workforce: WorkforceState | null,
      status: SystemStatus | null,
      replaceTasks: boolean
    ) => {
      setBoardError(false);
      setLoaded(true);
      setWorkforce(workforce);
      setSystemStatus(status);
      setBoardIssues({ failedProjects: board.failedProjects, cappedProjects: board.cappedProjects });
      // A project whose fetch failed keeps its last known tasks: dropping them
      // would make its cards vanish as if the work were gone.
      const failed = new Set(board.failedProjects.map((f) => f.projectId));
      setLocalTasks((prev) => {
        const kept = prev.filter((t) => failed.has(t.project_id));
        const fresh =
          replaceTasks || prev.length === 0
            ? board.tasks
            : board.tasks.map((serverTask: Task) => {
                const localTask = prev.find((t) => t.id === serverTask.id);
                return localTask && localTask.updated_at > serverTask.updated_at
                  ? localTask
                  : serverTask;
              });
        return [...fresh, ...kept];
      });
      setSelectedTask?.((prev) => {
        if (!prev) return prev;
        const fresh = board.tasks.find((t: Task) => t.id === prev.id);
        return fresh && fresh.updated_at > prev.updated_at ? fresh : prev;
      });
      return board.tasks;
    },
    [setSelectedTask]
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
        const boardErr = boardRes.status === "rejected" ? boardRes.reason : null;
        const workforceErr = workforceRes.status === "rejected" ? workforceRes.reason : null;
        const errMsg = boardErr
          ? `Failed to load board: ${boardErr}`
          : workforceErr
            ? `Failed to load workforce: ${workforceErr}`
            : "dashboard refresh failed";
        throw new Error(errMsg);
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

  // Replace one project's tasks after an SSE frame named it. A failed fetch is
  // left to the fallback poll.
  const refreshProject = useCallback(
    async (projectId: string): Promise<void> => {
      let fresh: { tasks: Task[]; capped: boolean };
      try {
        fresh = await getProjectTasks(projectId);
      } catch (e) {
        console.error("Project refresh failed", e);
        return;
      }
      setLocalTasks((prev) => [
        ...prev.filter((t) => t.project_id !== projectId),
        ...fresh.tasks.map((serverTask) => {
          const localTask = prev.find((t) => t.id === serverTask.id);
          return localTask && localTask.updated_at > serverTask.updated_at ? localTask : serverTask;
        }),
      ]);
      setBoardIssues((prev) => ({
        failedProjects: prev.failedProjects.filter((f) => f.projectId !== projectId),
        cappedProjects: [
          ...prev.cappedProjects.filter((id) => id !== projectId),
          ...(fresh.capped ? [projectId] : []),
        ],
      }));
      setSelectedTask?.((prev) => {
        if (!prev) return prev;
        const next = fresh.tasks.find((t) => t.id === prev.id);
        return next && next.updated_at > prev.updated_at ? next : prev;
      });
    },
    [setSelectedTask]
  );

  useEffect(() => {
    let mounted = true;
    let timerId: ReturnType<typeof setTimeout> | undefined;

    const poll = async () => {
      clearTimeout(timerId);
      timerId = undefined;
      try {
        await refreshDashboard(false);
      } catch (e) {
        console.error("Polling failed", e);
        if (mounted) setBoardError(true);
      } finally {
        // A hidden tab stops polling; the visibility handler resumes it.
        if (mounted && !document.hidden) timerId = setTimeout(poll, FALLBACK_POLL_MS);
      }
    };
    const onVisibility = () => {
      if (document.hidden) {
        clearTimeout(timerId);
        timerId = undefined;
      } else {
        void poll();
      }
    };

    void poll();
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      mounted = false;
      clearTimeout(timerId);
      document.removeEventListener("visibilitychange", onVisibility);
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
    boardIssues,
    daemonState: (boardError ? "unreachable" : loaded ? "online" : "connecting") as DaemonState,
    systemStatus,
    refreshDashboard,
    refreshProject,
    handleMaterialized,
  };
}
