"use client";

import { useState, useEffect } from "react";
import {
  MessageSquare,
  LayoutDashboard,
  Users,
  BookOpen,
  Settings,
  Terminal,
  Plus,
} from 'lucide-react';
import { motion, AnimatePresence } from "framer-motion";
import {
  Task,
  Project,
  WorkforceState,
  ChatMessage,
  DraftPlan,
} from '@/lib/types';
import { SidebarItem } from "@/app/components/sidebar-item";
import { ChatPanel } from "@/app/components/chat-panel";
import { BoardPanel } from "@/app/components/board-panel";
import { LogsPanel } from "@/app/components/logs-panel";
import { getBoard, getWorkforce, sendChat, postApprovePlan } from "@/lib/api";

export default function Page() {
  const [activeTab, setActiveTab] = useState('chat');
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [input, setInput] = useState('');
  const [isTyping, setIsTyping] = useState(false);
  const [boardData, setBoardData] = useState<{ projects: Project[], tasks: Task[] }>({ projects: [], tasks: [] });
  const [workforce, setWorkforce] = useState<WorkforceState | null>(null);
  const [draftPlan, setDraftPlan] = useState<DraftPlan | null>(null);

  useEffect(() => {
    let mounted = true;

    const poll = async () => {
      try {
        const [board, workforce] = await Promise.all([getBoard(), getWorkforce()]);
        if (!mounted) return;
        setBoardData(board);
        setWorkforce(workforce);
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

  const handleSend = async () => {
    if (!input.trim()) return;
    const userMsg: ChatMessage = { role: 'user', content: input };
    setMessages(prev => [...prev, userMsg]);
    setInput('');
    setIsTyping(true);

    try {
      const data = await sendChat(input);
      const assistantMessage =
        data?.message ??
        (data?.choices?.[0]?.message?.content
          ? { role: 'assistant', content: data.choices[0].message.content }
          : null);
      if (assistantMessage) setMessages(prev => [...prev, assistantMessage]);
      if (data.plan) {
        setDraftPlan(data.plan);
      }
  
    } catch (e) {
      setMessages(prev => [...prev, { role: 'assistant', content: "I'm having trouble connecting to the system core. Please try again." }]);
    } finally {
      setIsTyping(false);
    }
  };

  const approvePlan = async () => {
    try {
      await postApprovePlan();
      setDraftPlan(null);
      setMessages(prev => [...prev, { role: 'assistant', content: "The workforce has been deployed. You can track progress on the Board." }]);
      setActiveTab('board');
    } catch (e) { console.error(e); }
  };

  return (
    <div className="flex h-screen bg-bg font-sans text-text selection:bg-blue selection:text-bg">
      {/* Sidebar */}
      <aside className="w-60 border-r border-border bg-panel flex flex-col">
        <div className="p-4">
          <div className="flex items-center gap-3 mb-8 px-2">
            <div className="w-7 h-7 bg-accent rounded flex items-center justify-center text-white font-bold text-sm shadow-[0_0_15px_rgba(35,134,54,0.3)]">A</div>
            <h1 className="font-bold text-lg tracking-tight text-text">agentd</h1>
            <span className="ml-auto text-[9px] font-bold bg-accent/20 text-accent px-1.5 py-0.5 rounded-full border border-accent/30">Stable</span>
          </div>
          
          <nav className="space-y-1">
            <SidebarItem icon={MessageSquare} label="Intake Console" active={activeTab === 'chat'} onClick={() => setActiveTab('chat')} />
            <SidebarItem icon={LayoutDashboard} label="Workforce Board" active={activeTab === 'board'} onClick={() => setActiveTab('board')} />
            <SidebarItem icon={Users} label="Digital Workers" active={activeTab === 'workforce'} onClick={() => setActiveTab('workforce')} />
            <SidebarItem icon={BookOpen} label="Knowledge Index" active={activeTab === 'knowledge'} onClick={() => setActiveTab('knowledge')} />
            <SidebarItem icon={Terminal} label="System Kernel" active={activeTab === 'logs'} onClick={() => setActiveTab('logs')} />
          </nav>
        </div>

        <div className="mt-auto p-4 space-y-4">
          {workforce && (
            <div className="p-3 bg-bg/50 rounded-lg border border-border">
              <div className="flex justify-between items-center mb-1.5">
                <span className="text-[9px] font-bold text-text-dim uppercase tracking-widest">Load Status</span>
                <span className="text-[10px] font-mono text-text">{workforce.activeWorkers}/{workforce.maxWorkers}</span>
              </div>
              <div className="h-1 bg-border rounded-full overflow-hidden">
                <motion.div 
                   initial={{ width: 0 }}
                   animate={{
                     width: `${
                       workforce.maxWorkers > 0
                         ? Math.min(100, (workforce.activeWorkers / workforce.maxWorkers) * 100)
                         : 0
                     }%`
                   }}
                   className="h-full bg-blue" 
                />
              </div>
            </div>
          )}
          <SidebarItem icon={Settings} label="System Config" active={activeTab === 'settings'} onClick={() => setActiveTab('settings')} />
        </div>
      </aside>

      {/* Main Content */}
      <main className="flex-1 flex flex-col overflow-hidden relative">
        <header className="h-12 border-b border-border bg-panel px-6 flex items-center justify-between">
          <div className="flex items-center gap-3">
            <div className="flex items-center gap-2">
              <div className="w-1.5 h-1.5 rounded-full bg-accent shadow-[0_0_8px_var(--color-accent)] animate-pulse" />
              <span className="text-[10px] font-bold uppercase tracking-[0.15em] text-text-dim">Daemon Online</span>
            </div>
            <div className="h-4 w-px bg-border mx-1" />
            <span className="text-[10px] font-mono text-text-dim">REF: EXPR-API-V2</span>
          </div>
          
          <div className="flex items-center gap-3">
             <div className="text-[10px] text-text-dim font-mono hidden md:block">~/.agentd/projects/</div>
             <button className="px-3 py-1 text-[11px] font-bold text-white bg-accent rounded hover:bg-accent-hover transition-all flex items-center gap-1.5 shadow-sm">
                <Plus size={14} /> NEW INTAKE
             </button>
          </div>
        </header>

        <div className="flex-1 overflow-y-auto p-6 md:p-8">
          <AnimatePresence mode="wait">
            {activeTab === 'chat' && (
              <ChatPanel
                messages={messages}
                input={input}
                setInput={setInput}
                isTyping={isTyping}
                draftPlan={draftPlan}
                handleSend={handleSend}
                approvePlan={approvePlan}
                onReplan={() => setDraftPlan(null)}
              />
            )}

            {activeTab === 'board' && (
              <BoardPanel boardData={boardData} />
            )}

            {activeTab === 'logs' && (
              <LogsPanel boardData={boardData} />
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
      <footer className="fixed bottom-0 right-0 left-60 h-8 border-t border-border bg-panel flex items-center justify-between px-6 z-10">
         <div className="flex gap-6 items-center">
            <div className="flex items-center gap-1.5">
               <span className="text-[9px] font-bold text-text-dim uppercase tracking-widest">Daemon</span>
               <span className="text-[9px] font-bold text-accent">RUNNING</span>
            </div>
            <div className="flex items-center gap-1.5">
               <span className="text-[9px] font-bold text-text-dim uppercase tracking-widest">Workspace</span>
               <span className="text-[9px] font-mono text-text">~/.agentd/projects/</span>
            </div>
         </div>
         <div className="flex gap-6 items-center text-[9px] font-mono text-text-dim">
            <span>THREADS: 6/8</span>
            <span>UPTIME: 14h 22m</span>
            <span>STORAGE: 1.2GB Free</span>
         </div>
      </footer>
    </div>
  );
}

