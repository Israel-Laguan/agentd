"use client";

import { useState } from "react";
import { motion } from "framer-motion";
import {
  ChatMessage,
  DraftPlan,
  MaterializeResult,
} from "@/lib/types";

import { ChatMessageView } from "./chat-message";
import { ChatInput } from "./chat-input";
import { DraftPlanView } from "./draft-plan-view";
import { ChatTyping } from "./chat-typing";
import { ChatEmptyState } from "./chat-empty-state";
import { ChatStatusReportView } from "./chat-status-report-view";
import {
  ChatScopeClarificationPanel,
  ChatIntentClarificationPanel,
} from "./chat-clarification-panel";
import { Settings } from "lucide-react";
import {
  ChatSettingsModal,
  ChatSettings,
} from "./chat-settings-modal";
import { useChatSession } from "./use-chat-session";

interface ChatViewProps {
  messages: ChatMessage[];
  setMessages: React.Dispatch<React.SetStateAction<ChatMessage[]>>;
  draftPlan: DraftPlan | null;
  setDraftPlan: React.Dispatch<React.SetStateAction<DraftPlan | null>>;
  setActiveTab: (tab: string) => void;
  onMaterialized?: (result: MaterializeResult) => void | Promise<void>;
  input: string;
  setInput: (v: string) => void;
  isTyping: boolean;
  setIsTyping: (v: boolean) => void;
  inputRef?: React.RefObject<HTMLInputElement | null>;
  chatSettings: ChatSettings;
  setChatSettings: React.Dispatch<React.SetStateAction<ChatSettings>>;
}

export function ChatView({
  messages,
  setMessages,
  draftPlan,
  setDraftPlan,
  setActiveTab,
  onMaterialized,
  input,
  setInput,
  isTyping,
  setIsTyping,
  inputRef,
  chatSettings,
  setChatSettings
}: ChatViewProps) {
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [draftSettings, setDraftSettings] = useState(chatSettings);

  const {
    pendingScopeOptions,
    showIntentActions,
    statusReport,
    handleSend,
    handleScopeSelect,
    sendSuggested,
    approvePlan,
  } = useChatSession(
    chatSettings,
    setMessages,
    draftPlan,
    setDraftPlan,
    setActiveTab,
    onMaterialized
  );

  return (
    <motion.div
        key="chat"
        initial={{ opacity: 0, scale: 0.99 }}
        animate={{ opacity: 1, scale: 1 }}
        exit={{ opacity: 0, scale: 0.99 }}
        className="max-w-4xl mx-auto h-full flex flex-col"
    > 
      <div className="flex justify-end items-center gap-2">
        <span>Chat Settings</span>
        <button
          type="button"
          aria-label="Open chat settings"
          onClick={() => {
            setDraftSettings(chatSettings);
            setSettingsOpen(true);
          }}
          className="p-2 text-text-dim hover:text-text hover:border-text-dim transition-all"
        >
          <Settings size={20} />
        </button>

      </div>
      
      <div className="flex-1 space-y-6 mb-8 overflow-y-auto pb-4 px-2">
        {messages.length === 0 && (
          <ChatEmptyState setInput={setInput} />
        )}

        {messages.map((m, i) => (
          <ChatMessageView key={m.id ?? i} message={m} />
        ))}

        {isTyping && <ChatTyping />}

        {statusReport && <ChatStatusReportView report={statusReport} />}

        {pendingScopeOptions.length > 0 && (
          <ChatScopeClarificationPanel
            scopes={pendingScopeOptions}
            onSelect={(scopeId) => void handleScopeSelect(scopeId, setIsTyping)}
          />
        )}

        {showIntentActions && (
          <ChatIntentClarificationPanel
            onStatusCheck={() => void sendSuggested("What's the status of my projects?", setIsTyping)}
            onPlanWork={() => void sendSuggested("I'd like to plan new work for my project", setIsTyping)}
          />
        )}

        {draftPlan && (
          <DraftPlanView
            plan={draftPlan}
            onApprove={approvePlan}
            onReplan={() => setDraftPlan(null)}
          />
        )}
      </div>

      <ChatInput
        value={input}
        setValue={setInput}
        onSend={() => void handleSend(input, setInput, setIsTyping)}
        isTyping={isTyping}
        inputRef={inputRef}
      />

      <ChatSettingsModal
        open={settingsOpen}
        onClose={() => setSettingsOpen(false)}
        settings={draftSettings}
        setSettings={setDraftSettings}
        onSave={setChatSettings}
      />
    </motion.div>
  );
}
