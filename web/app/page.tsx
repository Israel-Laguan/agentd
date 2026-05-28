"use client";

import { useState, useEffect, useRef, useCallback } from "react";
import { LayoutDashboard } from 'lucide-react';
import { AnimatePresence } from "framer-motion";
import {
  Task,
  ChatMessage,
  DraftPlan,
} from '@/lib/types';
import { fetchProviders } from "@/lib/api";
import { BoardView } from "@/app/components/board/board-view";
import { ChatView } from "@/app/components/chat/chat-view";
import { LogsView } from "@/app/components/logs-view";
import { Footer } from "@/app/components/layout/footer";
import { Header } from "@/app/components/layout/header";
import { Sidebar } from "./components/layout/sidebar";
import { TaskDrawer } from "@/app/components/task/task-drawer";
import { ChatSettings } from "./components/chat/chat-settings-modal";
import { useDashboard } from "@/app/hooks/use-dashboard";
import { useTaskBoard } from "@/app/hooks/use-task-board";

const DEFAULT_CHAT_SETTINGS: ChatSettings = {
  provider: "openai",
  model: "gpt-4o",
  effort: "medium",
};

export default function Page() {
  const [activeTab, setActiveTab] = useState('chat');
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [draftPlan, setDraftPlan] = useState<DraftPlan | null>(null);
  const [selectedTask, setSelectedTask] = useState<Task | null>(null);
  const [input, setInput] = useState("");
  const [isTyping, setIsTyping] = useState(false);
  const [chatSettings, setChatSettings] = useState<ChatSettings>(DEFAULT_CHAT_SETTINGS);
  const inputRef = useRef<HTMLInputElement>(null);

  const {
    workforce,
    localTasks,
    setLocalTasks,
    boardError,
    systemStatus,
    handleMaterialized,
  } = useDashboard(setSelectedTask);

  const { handleDragEnd, handleUpdateTask } = useTaskBoard(
    localTasks,
    setLocalTasks,
    setSelectedTask
  );

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

  const onMaterialized = useCallback(
    (result: Parameters<typeof handleMaterialized>[0]) =>
      handleMaterialized(result, setSelectedTask),
    [handleMaterialized]
  );

  const onUpdateTask = async (id: string, patch: Partial<Task>) => {
    try {
      await handleUpdateTask(id, patch);
    } catch (err) {
      console.error("Failed to update task", err);
      throw err;
    }
  };

  return (
    <div className="flex h-screen bg-bg font-sans text-text selection:bg-blue selection:text-bg">
      <Sidebar 
        activeTab={activeTab} 
        setActiveTab={setActiveTab} 
        workforce={workforce} 
      />

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
                onMaterialized={onMaterialized}
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
        onUpdateTask={onUpdateTask}
      />
    </div>
  );
}
