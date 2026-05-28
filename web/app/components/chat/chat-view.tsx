"use client";

import { useState, useRef, useCallback } from "react";
import { motion } from "framer-motion";

function createMessageId() {
  return globalThis.crypto?.randomUUID?.() ?? `msg-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

import {
  ChatMessage,
  ChatResponse,
  ChatScopeOption,
  ChatStatusReport,
  DraftPlan,
  MaterializeResult,
} from "@/lib/types";
import { sendChat, postApprovePlan } from "@/lib/api";

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
  
  const [draftSettings, setDraftSettings] =
  useState(chatSettings);

  const [pendingScopeOptions, setPendingScopeOptions] = useState<ChatScopeOption[]>([]);
  const [showIntentActions, setShowIntentActions] = useState(false);
  const [statusReport, setStatusReport] = useState<ChatStatusReport | null>(null);
  const lastUserMessageRef = useRef<string>("");

  const clearStructuredUi = useCallback(() => {
    setPendingScopeOptions([]);
    setShowIntentActions(false);
    setStatusReport(null);
  }, []);

  const applyChatResponse = useCallback(
    (data: ChatResponse) => {
      if (data.plan) {
        setDraftPlan(data.plan);
        setStatusReport(null);
        setPendingScopeOptions([]);
        setShowIntentActions(false);
        return;
      }
      setDraftPlan(null);
      if (data.statusReport) {
        setStatusReport(data.statusReport);
        setPendingScopeOptions([]);
        setShowIntentActions(false);
        return;
      }
      setStatusReport(null);
      if (data.scopeClarification?.scopes.length) {
        setPendingScopeOptions(data.scopeClarification.scopes);
        setShowIntentActions(false);
        return;
      }
      setPendingScopeOptions([]);
      setShowIntentActions(Boolean(data.intentClarification));
    },
    [setDraftPlan]
  );

  const assistantFromResponse = (data: ChatResponse): ChatMessage =>
    data?.message
      ? { ...data.message, id: data.message.id ?? createMessageId() } as ChatMessage
      : {
          id: createMessageId(),
          role: "assistant",
          content: "Sorry, I couldn't get a response — please try again.",
        };

  const runChat = useCallback(
    async (message: string, approvedScopes?: string[]) => {
      const data = await sendChat(message, chatSettings, approvedScopes);
      const assistant = assistantFromResponse(data);
      setMessages((p) => [...p, assistant]);
      applyChatResponse(data);
      return data;
    },
    [chatSettings, setMessages, applyChatResponse]
  );

  const handleSend = async () => {
    if (!input.trim()) return;

    const userMsg = { id: createMessageId(), role: "user", content: input } as ChatMessage;
    lastUserMessageRef.current = input;

    setMessages((p) => [...p, userMsg]);
    setInput("");
    setIsTyping(true);
    clearStructuredUi();

    try {
      await runChat(userMsg.content);
    } catch (error) {
      console.error("Failed to send message:", error);
      setMessages((p) => [
        ...p,
        {
          id: createMessageId(),
          role: "assistant",
          content: "Sorry, I encountered an error processing your request. Please try again.",
        },
      ]);
    } finally {
      setIsTyping(false);
    }
  };

  const handleScopeSelect = async (scopeId: string) => {
    setPendingScopeOptions([]);
    const originalMessage = lastUserMessageRef.current;
    if (!originalMessage) return;

    const userMsg = {
      id: createMessageId(),
      role: "user" as const,
      content: `[Scope selected: ${scopeId}]`,
    };
    setMessages((p) => [...p, userMsg]);
    setIsTyping(true);
    clearStructuredUi();

    try {
      await runChat(originalMessage, [scopeId]);
    } catch (error) {
      console.error("Failed to send scope selection:", error);
    } finally {
      setIsTyping(false);
    }
  };

  const sendSuggested = async (text: string) => {
    lastUserMessageRef.current = text;
    const userMsg = { id: createMessageId(), role: "user", content: text };
    setMessages((p) => [...p, userMsg]);
    setIsTyping(true);
    clearStructuredUi();

    try {
      await runChat(text);
    } catch (error) {
      console.error("Failed to send suggested message:", error);
    } finally {
      setIsTyping(false);
    }
  };

  const approvePlan = async () => {
    if (!draftPlan) return;
    try {
      const result = await postApprovePlan(draftPlan);
      setDraftPlan(null);
      clearStructuredUi();
      setMessages((p) => [
        ...p,
        {
          id: createMessageId(),
          role: "assistant",
          content: "The workforce has been deployed. You can track progress on the board.",
        },
      ]);
      await onMaterialized?.(result);
      setActiveTab("board");
    } catch (e) {
      console.error(e);
      setMessages((p) => [
        ...p,
        {
          id: createMessageId(),
          role: "assistant",
          content: "Failed to materialize the plan. Check the daemon logs and try again.",
        },
      ]);
    }
  };

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
      
      {/* Messages */}
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
            onSelect={handleScopeSelect}
          />
        )}

        {showIntentActions && (
          <ChatIntentClarificationPanel
            onStatusCheck={() => void sendSuggested("What's the status of my projects?")}
            onPlanWork={() => void sendSuggested("I'd like to plan new work for my project")}
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

      {/* Input */}
      <ChatInput
        value={input}
        setValue={setInput}
        onSend={handleSend}
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
