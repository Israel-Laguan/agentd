"use client";

import { useEffect, useRef, useState } from "react";
import { API, USE_MOCK } from "@/lib/api-config";
import { BOARD_ROW_EVENT_NAMES, TASK_DETAIL_EVENT_NAMES, parseSseEnvelope } from "@/lib/sse-events";

// Frames arrive in bursts (a task finishing emits several); wait this long so
// one burst costs one refetch.
const COALESCE_MS = 250;
// A running task streams log and tool frames continuously; the open drawer's
// event list re-reads at most once per this window.
const DETAIL_THROTTLE_MS = 1000;
const REOPEN_MS = 3000;

const ROW_EVENTS: readonly string[] = BOARD_ROW_EVENT_NAMES;
const ALL_EVENTS: readonly string[] = [...BOARD_ROW_EVENT_NAMES, ...TASK_DETAIL_EVENT_NAMES];

interface BoardStreamOptions {
  selectedTaskId?: string;
  // Refetch one project's tasks.
  refreshProject: (projectId: string) => void;
  // Refetch everything: events may have been missed while the stream was down.
  resync: () => void;
}

/**
 * useBoardStream keeps the board fresh from the daemon's SSE stream instead of
 * polling: a frame that changes a task row refetches just that frame's project,
 * and activity on the selected task bumps `taskActivityKey` (throttled) so the
 * open drawer re-reads its events, and `commentActivityKey` (only for
 * comment_added) so it re-reads its comments. After the stream drops and returns it asks
 * for a full resync. No-op in mock mode or where EventSource is missing.
 */
export function useBoardStream({ selectedTaskId, refreshProject, resync }: BoardStreamOptions) {
  const [taskActivityKey, setTaskActivityKey] = useState(0);
  const [commentActivityKey, setCommentActivityKey] = useState(0);
  const latest = useRef({ selectedTaskId, refreshProject, resync });
  useEffect(() => {
    latest.current = { selectedTaskId, refreshProject, resync };
  });

  useEffect(() => {
    if (USE_MOCK || typeof EventSource === "undefined") return;
    let stopped = false;
    let wasDown = false;
    let source: EventSource | null = null;
    let reopenTimer: ReturnType<typeof setTimeout> | undefined;
    let flushTimer: ReturnType<typeof setTimeout> | undefined;
    let detailTimer: ReturnType<typeof setTimeout> | undefined;
    const projects = new Set<string>();
    let commentsTouched = false;
    let taskTouched = false;

    const flush = () => {
      flushTimer = undefined;
      for (const id of projects) latest.current.refreshProject(id);
      projects.clear();
      if (commentsTouched) setCommentActivityKey((k) => k + 1);
      commentsTouched = false;
    };
    const flushDetail = () => {
      detailTimer = undefined;
      if (taskTouched) setTaskActivityKey((k) => k + 1);
      taskTouched = false;
    };

    const onFrame = (name: string, data: string) => {
      const env = parseSseEnvelope(data);
      if (!env) return;
      if (env.project_id && ROW_EVENTS.includes(name)) projects.add(env.project_id);
      if (env.task_id && env.task_id === latest.current.selectedTaskId) {
        taskTouched = true;
        if (name === "comment_added") commentsTouched = true;
      }
      if (!flushTimer && (projects.size > 0 || commentsTouched)) flushTimer = setTimeout(flush, COALESCE_MS);
      if (!detailTimer && taskTouched) detailTimer = setTimeout(flushDetail, DETAIL_THROTTLE_MS);
    };

    const connect = () => {
      const es = new EventSource(`${API}/api/v1/events/stream`);
      source = es;
      const handlers = ALL_EVENTS.map((name) => {
        const handler = (ev: MessageEvent) => onFrame(name, ev.data);
        es.addEventListener(name, handler);
        return [name, handler] as const;
      });
      es.onopen = () => {
        if (wasDown) latest.current.resync();
        wasDown = false;
      };
      es.onerror = () => {
        wasDown = true;
        // The browser retries on its own unless it has given up (CLOSED).
        if (es.readyState !== 2 || stopped) return;
        for (const [name, handler] of handlers) es.removeEventListener(name, handler);
        es.close();
        reopenTimer = setTimeout(connect, REOPEN_MS);
      };
    };

    connect();
    return () => {
      stopped = true;
      clearTimeout(reopenTimer);
      clearTimeout(flushTimer);
      clearTimeout(detailTimer);
      if (source) {
        source.onopen = null;
        source.onerror = null;
        source.close();
      }
    };
  }, []);

  return { taskActivityKey, commentActivityKey };
}
