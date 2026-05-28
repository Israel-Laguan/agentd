"use client";

import { DragEndEvent } from "@dnd-kit/core";
import { useState, useEffect, useRef, useCallback } from "react";
import { LayoutDashboard } from 'lucide-react';
import { AnimatePresence } from "framer-motion";
import {
  TaskStatus,
  Task,
  WorkforceState,
  ChatMessage,
  DraftPlan,
  SystemStatus,
} from '@/lib/types';
import { getBoard, getWorkforce, getSystemStatus, updateTask, fetchProviders } from "@/lib/api";
import { BoardView } from "@/app/components/board/board-view";
import { ChatView } from "@/app/components/chat/chat-view";
import { LogsView } from "@/app/components/logs-view";
import { Footer } from "@/app/components/layout/footer";
import { Header } from "@/app/components/layout/header";
import { Sidebar } from "./components/layout/sidebar";
import { TaskDrawer } from "@/app/components/task/task-drawer";
import { ChatSettings } from "./components/chat/chat-settings-modal";

const DEFAULT_CHAT_SETTINGS: ChatSettings = {
  provider: "openai",
  model: "gpt-4o",
  effort: "medium",
};

export default function Page() {
  const [activeTab, setActiveTab] = useState('chat');
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [workforce, setWorkforce] = useState<WorkforceState | null>(null);
  const [draftPlan, setDraftPlan] = useState<DraftPlan | null>(null);
  const [localTasks, setLocalTasks] = useState<Task[]>([]);
  const [selectedTask, setSelectedTask] = useState<Task | null>(null);
  const [boardError, setBoardError] = useState(false);
  const [systemStatus, setSystemStatus] = useState<SystemStatus | null>(null);
  const [input, setInput] = useState("");
  const [isTyping, setIsTyping] = useState(false);
  const [chatSettings, setChatSettings] = useState<ChatSettings>(DEFAULT_CHAT_SETTINGS);
  const inputRef = useRef<HTMLInputElement>(null);
  /** Per-task optimistic drag generation; avoids rollback when Date.now() collides. */
  const dragVersionRef = useRef<Map<string, number>>(new Map());

  // Seed chat settings from the first configured provider once the daemon responds.
  // Only applies when the user has not already changed away from the default.
  useEffect(() => {
    fetchProviders().then((list) => {
      const first = list[0];
      setChatSettings(prev =>
        (prev.provider !== DEFAULT_CHAT_SETTINGS.provider || prev.model !== DEFAULT_CHAT_SETTINGS.model)
          ? prev
          : first
            ? { ...prev, provider: first.name, model: first.models[0] ?? "" }
            : prev
      );
    }).catch(() => {
      // Daemon unreachable in USE_MOCK=false; keep existing defaults silently.
    });
  }, []);

  const handleNewIntake = useCallback(() => {
    setActiveTab('chat');
    setMessages([]);
    setDraftPlan(null);
    setInput('');
    setSelectedTask(null);
    setChatSettings(prev => ({ provider: prev.provider, model: prev.model, effort: "medium" }));
    requestAnimationFrame(() => inputRef.current?.focus());
  }, []);

  useEffect(() => {
    let mounted = true;

    const poll = async () => {
      try {
        const [boardRes, workforceRes, statusRes] = await Promise.allSettled([
          getBoard(),
          getWorkforce(),
          getSystemStatus(),
        ]);
        if (!mounted) return;
        if (boardRes.status !== "fulfilled" || workforceRes.status !== "fulfilled") {
          setBoardError(true);
          setSystemStatus(statusRes.status === "fulfilled" ? statusRes.value : null);
          return;
        }
        const board = boardRes.value;
        const workforce = workforceRes.value;
        setBoardError(false);
        setWorkforce(workforce);
        setSystemStatus(statusRes.status === "fulfilled" ? statusRes.value : null);

        setLocalTasks(prev =>
          prev.length === 0
            ? board.tasks
            : board.tasks.map((serverTask: Task) => {
                const localTask = prev.find(t => t.id === serverTask.id);
                return localTask && localTask.updated_at > serverTask.updated_at
                  ? localTask
                  : serverTask;
              })
        );
        setSelectedTask(prev => {
          if (!prev) return prev;
          const fresh = board.tasks.find((t: Task) => t.id === prev.id);
          return fresh && fresh.updated_at > prev.updated_at ? fresh : prev;
        });
      } catch (e) {
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
  }, []);

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
    try {
      const updated = await updateTask(id, patch);
      setLocalTasks((prev) => prev.map((t) => (t.id === id ? updated : t)));
      setSelectedTask((prev) => (prev?.id === id ? updated : prev));
    } catch (err) {
      console.error("Failed to update task", err);
      throw err;
    }
  };

  return (
    <div className="flex h-screen bg-bg font-sans text-text selection:bg-blue selection:text-bg">
      {/* Sidebar */}
      <Sidebar 
        activeTab={activeTab} 
        setActiveTab={setActiveTab} 
        workforce={workforce} 
      />

      {/* Main Content */}
      <main className="flex-1 flex flex-col overflow-hidden relative">
        <Header onStartNewIntake={handleNewIntake} />

        <div className="flex-1 overflow-y-auto p-6 md:p-8">
          <AnimatePresence mode="wait">
            {activeTab === 'chat' && (
              <ChatView
                messages={messages}
                setMessages={setMessages}
                draftPlan={draftPlan}
                setDraftPlan={setDraftPlan}
                setActiveTab={setActiveTab}
                input={input}
                setInput={setInput}
                isTyping={isTyping}
                setIsTyping={setIsTyping}
                inputRef={inputRef}
                chatSettings={chatSettings}
                setChatSettings={setChatSettings}
              />
            )}

            {activeTab === 'board' && (
              <>
                {boardError && (
                  <div className="mx-4 mt-3 px-3 py-2 rounded-md bg-red-500/10 border border-red-500/30 text-red-400 text-xs">
                    Could not reach daemon — board may be stale. Retrying&hellip;
                  </div>
                )}
                <BoardView
                  tasks={localTasks}
                  onDragEnd={handleDragEnd}
                  onTaskClick={setSelectedTask}
                />
              </>
            )}

            {activeTab === 'logs' && (
              <LogsView tasks={localTasks} />
            )}

            {/* Other tabs placeholder */}
            {(activeTab === 'workforce' || activeTab === 'knowledge' || activeTab === 'settings') && (
              <div className="flex flex-col items-center justify-center h-full text-center py-20">
                <LayoutDashboard size={40} className="text-border mb-6" />
                <h2 className="text-xl font-bold text-text uppercase tracking-tight mb-2">{activeTab} Module</h2>
                <p className="text-text-dim text-xs max-w-sm">Access Restricted: User profile lacks clearance for this sector in the v1.x core kernel.</p>
              </div>
            )}
          </AnimatePresence>
        </div>
      </main>

      {/* Persistent System Footer */}
      <Footer
        daemonReachable={!boardError}
        totalTokens={systemStatus?.total_token_usage ?? null}
        runningTasks={
          systemStatus?.status?.summary.tasks_by_state?.RUNNING ?? null
        }
        rollingBudget={
          systemStatus?.rolling_budget_enabled &&
          systemStatus.rolling_token_limit &&
          systemStatus.rolling_token_remaining !== undefined
            ? {
                limit: systemStatus.rolling_token_limit,
                remaining: systemStatus.rolling_token_remaining,
              }
            : null
        }
      />
      <TaskDrawer
        task={selectedTask}
        onClose={() => setSelectedTask(null)}
        onUpdateTask={handleUpdateTask}
      />
    </div>
  );
}

