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
} from '@/lib/types';
import { getBoard, getWorkforce, updateTask } from "@/lib/api";
import { BoardView } from "@/app/components/board/board-view";
import { ChatView } from "@/app/components/chat/chat-view";
import { LogsView } from "@/app/components/logs-view";
import { Footer } from "@/app/components/layout/footer";
import { Header } from "@/app/components/layout/header";
import { Sidebar } from "./components/layout/sidebar";
import { TaskDrawer } from "@/app/components/task/task-drawer";

export default function Page() {
  const [activeTab, setActiveTab] = useState('chat');
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [workforce, setWorkforce] = useState<WorkforceState | null>(null);
  const [draftPlan, setDraftPlan] = useState<DraftPlan | null>(null);
  const [localTasks, setLocalTasks] = useState<Task[]>([]);
  const [selectedTask, setSelectedTask] = useState<Task | null>(null);
  const [input, setInput] = useState("");
  const [isTyping, setIsTyping] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  const handleNewIntake = useCallback(() => {
    setActiveTab('chat');
    setMessages([]);
    setDraftPlan(null);
    setInput('');
    setSelectedTask(null);
    requestAnimationFrame(() => inputRef.current?.focus());
  }, []);

  useEffect(() => {
    let mounted = true;

    const poll = async () => {
      try {
        const [board, workforce] = await Promise.all([getBoard(), getWorkforce()]);
        if (!mounted) return;
        setWorkforce(workforce);

        setLocalTasks(prev =>
          prev.length === 0
            ? board.tasks
            : board.tasks.map((serverTask: Task) => {
                const localTask = prev.find(t => t.id === serverTask.id);
                return localTask && localTask.updatedAt > serverTask.updatedAt
                  ? localTask
                  : serverTask;
              })
        );
      } catch (e) {
        console.error("Polling failed", e);
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
    const prevStatus = currentTask?.status;
    const prevUpdatedAt = currentTask?.updatedAt;

    setLocalTasks((tasks) =>
      tasks.map((task) =>
        task.id === taskId
          ? {
              ...task,
              status: newStatus,
              updatedAt: Date.now(),
            }
          : task
      )
    );

    updateTask(taskId, { status: newStatus, updatedAt: Date.now() }).catch((err) => {
      console.error("Failed to persist task status", err);
      if (prevStatus !== undefined && prevUpdatedAt !== undefined) {
        setLocalTasks((tasks) =>
          tasks.map((task) =>
            task.id === taskId
              ? {
                  ...task,
                  status: prevStatus,
                  updatedAt: prevUpdatedAt,
                }
              : task
          )
        );
      }
    });
  };

  const handleUpdateTask = async (id: string, patch: Partial<Task>) => {
    try {
      const updated = await updateTask(id, patch);
      setLocalTasks((prev) => prev.map((t) => (t.id === id ? updated : t)));
    } catch (err) {
      console.error("Failed to update task", err);
    }
  };

  return (
    <div className="flex h-screen bg-bg font-sans text-text selection:bg-blue selection:text-bg">
      {/* Sidebar */}
      <Sidebar activeTab={activeTab} setActiveTab={setActiveTab} workforce={workforce} />

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
              />
            )}

            {activeTab === 'board' && (
              <BoardView
                tasks={localTasks}
                onDragEnd={handleDragEnd}
                onTaskClick={setSelectedTask}
              />
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
      <Footer />
      <TaskDrawer
        task={selectedTask}
        onClose={() => setSelectedTask(null)}
        onUpdateTask={handleUpdateTask}
      />
    </div>
  );
}

