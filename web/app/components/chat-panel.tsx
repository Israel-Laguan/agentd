"use client";

import { MessageSquare, CheckCircle2, ChevronRight, Send } from 'lucide-react';
import { motion } from "framer-motion";
import { cn, ChatMessage, DraftPlan } from '@/lib/types';

interface ChatPanelProps {
  messages: ChatMessage[];
  input: string;
  setInput: (s: string) => void;
  isTyping: boolean;
  draftPlan: DraftPlan | null;
  handleSend: () => void;
  approvePlan: () => void;
  onReplan: () => void;
}

export function ChatPanel({ messages, input, setInput, isTyping, draftPlan, handleSend, approvePlan, onReplan }: ChatPanelProps) {
  return (
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
              <button onClick={onReplan} className="px-4 py-2 border border-border text-text-dim text-[11px] font-bold rounded hover:bg-bg transition-all uppercase">
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
          aria-label="Chat input"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && handleSend()}
          placeholder="Input organizational goal..."
          className="flex-1 bg-transparent border-none focus:ring-0 text-sm py-3 text-text placeholder:text-text-dim/30"
        />
        <button
          type="button"
          aria-label="Send message"
          onClick={handleSend}
          className="h-9 px-4 bg-accent text-white rounded-lg flex items-center justify-center hover:bg-accent-hover transition-all mr-1 shadow-md"
        >
          <Send size={16} />
        </button>
      </div>
    </motion.div>
  );
}
