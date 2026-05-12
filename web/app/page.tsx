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
  CheckCircle2, 
  ChevronRight,
  Send
} from 'lucide-react';
import { motion, AnimatePresence } from "framer-motion";
import { 
  cn,
  TaskStatus, 
  Task, 
  Project, 
  WorkforceState,
  ChatMessage,
  DraftPlan, 
} from '@/lib/types';
import { SidebarItem } from "@/app/components/sidebar-item";
import { TaskCard } from "@/app/components/task-card";
import { getBoard, getWorkforce, sendChat, postApprovePlan } from "@/lib/api";

export default function Page() {
  const [activeTab, setActiveTab] = useState('chat');
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [input, setInput] = useState('');
  const [isTyping, setIsTyping] = useState(false);
  const [boardData, setBoardData] = useState<{ projects: Project[], tasks: Task[] }>({ projects: [], tasks: [] });
  const [workforce, setWorkforce] = useState<WorkforceState | null>(null);
  const [draftPlan, setDraftPlan] = useState<DraftPlan | null>(null);

  const fetchBoard = async () => {
    const data = await getBoard();
    setBoardData(data);
  };

  const fetchWorkforce = async () => {
    const data = await getWorkforce();
    setWorkforce(data);
  };
useEffect(() => {
  let mounted = true;

  const init = async () => {
    await Promise.all([fetchBoard(), fetchWorkforce()]);
  };

  init();

  const interval = setInterval(() => {
    if (!mounted) return;
    fetchBoard();
    fetchWorkforce();
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
      const res = await sendChat(input);
      const data = await res.json();
      setMessages(prev => [...prev, { role: 'assistant', content: data.response }]);
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
                   animate={{ width: `${(workforce.activeWorkers / workforce.maxWorkers) * 100}%` }}
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
              <motion.div 
                key="chat"
                initial={{ opacity: 0, scale: 0.99 }}
                animate={{ opacity: 1, scale: 1 }}
                exit={{ opacity: 0, scale: 0.99 }}
                className="max-w-4xl mx-auto h-full flex flex-col"
              >
                <div className="flex-1 space-y-6 mb-8 overflow-y-auto pb-4 px-2">
                  {messages.length === 0 && (
                    <div className="flex flex-col items-center justify-center h-full text-center py-20">
                      <div className="w-16 h-16 bg-panel border border-border rounded-3xl flex items-center justify-center text-accent mb-6 shadow-xl">
                        <MessageSquare size={32} />
                      </div>
                      <h2 className="text-2xl font-bold text-text mb-2">Digital Workforce Portal</h2>
                      <p className="text-text-dim text-sm max-w-sm">Enter your business intent. Our agent logic will plan, delegate, and execute autonomously.</p>
                      <div className="mt-8 grid grid-cols-1 sm:grid-cols-3 gap-2 w-full max-w-2xl px-4">
                        {["Set up a personal website", "Audit my server security", "Write a marketing script"].map(t => (
                          <button key={t} onClick={() => setInput(t)} className="px-4 py-2 bg-panel border border-border rounded-lg text-[11px] text-text-dim hover:border-accent hover:text-text transition-all text-center uppercase tracking-wider font-bold">
                            {t}
                          </button>
                        ))}
                      </div>
                    </div>
                  )}

                  {messages.map((m, i) => (
                    <div key={i} className={cn("flex gap-3", m.role === 'user' ? "flex-row-reverse" : "")}>
                       <div className={cn("w-7 h-7 rounded flex items-center justify-center shrink-0 font-bold text-[10px]", m.role === 'user' ? "bg-border text-text-dim" : "bg-accent text-white")}>
                          {m.role === 'user' ? "USR" : "SYS"}
                       </div>
                       <div className={cn("max-w-[85%] px-4 py-3 rounded-xl text-sm leading-relaxed", m.role === 'user' ? "bg-panel border border-border text-text" : "bg-bg border border-border text-text shadow-sm")}>
                          <p>{m.content}</p>
                       </div>
                    </div>
                  ))}

                  {isTyping && (
                    <div className="flex gap-3">
                      <div className="w-7 h-7 rounded bg-accent text-white flex items-center justify-center animate-pulse text-[10px] font-bold">SYS</div>
                      <div className="px-4 py-2 rounded-xl bg-bg border border-border shadow-sm flex items-center">
                        <div className="flex gap-1">
                          <div className="w-1 h-1 bg-accent rounded-full animate-bounce [animation-delay:-0.3s]" />
                          <div className="w-1 h-1 bg-accent rounded-full animate-bounce [animation-delay:-0.15s]" />
                          <div className="w-1 h-1 bg-accent rounded-full animate-bounce" />
                        </div>
                      </div>
                    </div>
                  )}

                  {draftPlan && (
                    <motion.div 
                      initial={{ y: 20, opacity: 0 }}
                      animate={{ y: 0, opacity: 1 }}
                      className="ml-10 p-5 bg-panel border border-accent/30 rounded-xl shadow-2xl relative overflow-hidden"
                    >
                      <div className="absolute top-0 left-0 w-full h-[2px] bg-accent" />
                      <div className="flex items-center justify-between mb-4">
                        <h3 className="font-bold text-text flex items-center gap-2 text-sm uppercase tracking-wider">
                          <CheckCircle2 size={16} className="text-accent" />
                          Workforce Allocation Plan
                        </h3>
                      </div>
                      
                      <div className="mb-4 bg-bg/50 p-3 rounded border border-border">
                        <h4 className="text-xs font-bold text-blue mb-1">{draftPlan.name}</h4>
                        <p className="text-[11px] text-text-dim leading-normal">{draftPlan.description}</p>
                      </div>

                      <div className="space-y-1.5 mb-5">
                        {draftPlan.tasks.map((t, i: number) => (
                          <div key={i} className="flex gap-3 p-2.5 bg-bg/30 border border-border rounded-lg group hover:border-text-dim/20 transition-colors">
                            <span className="text-[10px] font-mono flex items-center justify-center text-text-dim group-hover:text-accent">0{i + 1}</span>
                            <div>
                              <h5 className="text-[11px] font-bold text-text uppercase tracking-tight">{t.title}</h5>
                              <p className="text-[10px] text-text-dim leading-tight">{t.description}</p>
                            </div>
                          </div>
                        ))}
                      </div>

                      <div className="flex gap-2">
                        <button onClick={approvePlan} className="flex-1 py-2 bg-accent text-white text-[11px] font-bold rounded hover:bg-accent-hover transition-all uppercase tracking-widest shadow-lg shadow-accent/10">
                          EXECUTE STRATEGY
                        </button>
                        <button onClick={() => setDraftPlan(null)} className="px-4 py-2 border border-border text-text-dim text-[11px] font-bold rounded hover:bg-bg transition-all uppercase">
                          REPLAN
                        </button>
                      </div>
                    </motion.div>
                  )}
                </div>

                <div className="p-1 px-1.5 bg-panel border border-border rounded-xl shadow-2xl flex items-center gap-2 backdrop-blur-sm">
                  <div className="w-8 h-8 flex items-center justify-center text-text-dim/50">
                    <ChevronRight size={18} />
                  </div>
                  <input 
                    type="text" 
                    value={input}
                    onChange={(e) => setInput(e.target.value)}
                    onKeyPress={(e) => e.key === 'Enter' && handleSend()}
                    placeholder="Input organizational goal..." 
                    className="flex-1 bg-transparent border-none focus:ring-0 text-sm py-3 text-text placeholder:text-text-dim/30"
                  />
                  <button onClick={handleSend} className="h-9 px-4 bg-accent text-white rounded-lg flex items-center justify-center hover:bg-accent-hover transition-all mr-1 shadow-md">
                    <Send size={16} />
                  </button>
                </div>
              </motion.div>
            )}

            {activeTab === 'board' && (
              <motion.div 
                key="board"
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                className="h-full flex flex-col"
              >
                <div className="flex items-center justify-between mb-8">
                   <div>
                      <h2 className="text-xl font-bold text-text uppercase tracking-tight">Active Operation Board</h2>
                      <p className="text-[11px] text-text-dim font-mono mt-1">REAL-TIME WORKFORCE DISPATCH SYSTEM</p>
                   </div>
                   <div className="flex items-center gap-2">
                      <div className="flex -space-x-2">
                         {[1,2,3].map(i => <div key={i} className="w-6 h-6 rounded-full bg-border border-2 border-panel flex items-center justify-center text-[8px] font-bold">W{i}</div>)}
                      </div>
                      <span className="ml-2 px-2 py-0.5 bg-blue/10 text-blue text-[9px] font-bold rounded border border-blue/20 uppercase tracking-widest">Queue Healthy</span>
                   </div>
                </div>

                <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4 h-full content-start overflow-x-auto min-w-full pb-10">
                   {[TaskStatus.PENDING, TaskStatus.RUNNING, TaskStatus.COMPLETED, TaskStatus.FAILED].map(status => (
                     <div key={status} className="flex flex-col gap-3 min-w-[240px]">
                        <div className="flex justify-between items-center px-1 py-1.5 border-b border-border shadow-[0_1px_0_var(--color-bg)]">
                           <h3 className={cn(
                             "text-[10px] font-bold uppercase tracking-[0.2em]",
                             status === TaskStatus.RUNNING ? "text-blue" : "text-text-dim"
                           )}>{status}</h3>
                           <span className="text-[9px] font-mono bg-border/40 px-1.5 rounded text-text-dim">{boardData.tasks.filter(t => t.status === status).length}</span>
                        </div>
                        <div className="space-y-3 px-0.5">
                           {boardData.tasks
                             .filter(t => t.status === status)
                             .sort((a,b) => b.updatedAt - a.updatedAt)
                             .map(task => (
                               <TaskCard key={task.id} task={task} />
                             ))
                           }
                           {boardData.tasks.filter(t => t.status === status).length === 0 && (
                             <div className="h-24 border border-dashed border-border/50 rounded-md flex items-center justify-center bg-bg/20">
                               <span className="text-[9px] font-bold text-text-dim/30 uppercase tracking-widest">Idle</span>
                             </div>
                           )}
                        </div>
                     </div>
                   ))}
                </div>
              </motion.div>
            )}

            {activeTab === 'logs' && (
               <motion.div 
               key="logs"
               initial={{ opacity: 0 }}
               animate={{ opacity: 1 }}
               className="h-full flex flex-col font-mono"
             >
               <div className="flex-1 bg-bg border border-border rounded-xl p-6 overflow-y-auto text-[11px] leading-relaxed shadow-inner">
                  <div className="mb-4 text-text-dim/40 italic flex justify-between items-center border-b border-border pb-2">
                    <span>agentd Kernel View - Task Execution Stream</span>
                    <span className="animate-pulse">● System Live</span>
                  </div>
                  {boardData.tasks.flatMap(t => t.logs).slice(-150).map((log, i) => (
                    <div key={i} className="flex gap-3 hover:bg-white/5 py-0.5 px-1 rounded transition-colors group">
                       <span className="text-blue/40 shrink-0 select-none">[{new Date().toLocaleTimeString()}]</span>
                       <span className={cn(
                         "flex-1",
                         log.startsWith('[SYSTEM]') ? "text-blue" :
                         log.startsWith('[AGENT]') ? "text-text" :
                         log.startsWith('[ERROR]') ? "text-error font-bold" :
                         "text-text-dim"
                       )}>
                         {log}
                       </span>
                    </div>
                  ))}
                  <div className="h-4" />
               </div>
             </motion.div>
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

