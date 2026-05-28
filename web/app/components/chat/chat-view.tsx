"use client";

import { useState, useRef } from "react";
import { motion } from "framer-motion";

function createMessageId() {
  return globalThis.crypto?.randomUUID?.() ?? `msg-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

import { ChatMessage, DraftPlan } from "@/lib/types";
import { sendChat, postApprovePlan } from "@/lib/api";

import { ChatMessageView } from "./chat-message";
import { ChatInput } from "./chat-input";
import { DraftPlanView } from "./draft-plan-view";
import { ChatTyping } from "./chat-typing";
import { ChatEmptyState } from "./chat-empty-state";
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

  // approvedScopes: set when the user selects a scope from a scope_clarification
  // response; forwarded on the next sendChat call then cleared.
  const [approvedScopes, setApprovedScopes] = useState<string[]>([]);
  // pendingScopeOptions: populated when daemon returns scope_clarification JSON.
  const [pendingScopeOptions, setPendingScopeOptions] = useState<
    { id: string; description: string }[]
  >([]);
  // lastUserMessage: stored so scope selection can re-send the original intent.
  const lastUserMessageRef = useRef<string>("");

  const handleSend = async () => {
    if (!input.trim()) return;

    const userMsg = { id: createMessageId(), role: "user", content: input } as ChatMessage;
    lastUserMessageRef.current = input;

    setMessages((p) => [...p, userMsg]);
    setInput("");
    setIsTyping(true);
    setPendingScopeOptions([]);

    const scopesToSend = approvedScopes.slice();
    setApprovedScopes([]);

    try {
      const data = await sendChat(userMsg.content, chatSettings, scopesToSend.length > 0 ? scopesToSend : undefined);

      const assistant: ChatMessage =
        data?.message
          ? { ...data.message, id: data.message.id ?? createMessageId() } as ChatMessage
          : {
              id: createMessageId(),
              role: "assistant",
              content: "Sorry, I couldn't get a response — please try again.",
            };

      setMessages((p) => [...p, assistant]);
      if (data.plan) {
        setDraftPlan(data.plan);
      } else if (assistant.content) {
        // Check for scope_clarification / intent_clarification structured responses.
        try {
          const parsed = JSON.parse(assistant.content) as Record<string, unknown>;
          if (parsed.kind === "scope_clarification" && Array.isArray(parsed.scopes)) {
            setPendingScopeOptions(
              (parsed.scopes as Record<string, unknown>[]).map((s) => ({
                id: String(s.id ?? ""),
                description: String(s.description ?? ""),
              }))
            );
          }
        } catch {
          // plain text — ignore
        }
      }
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

    try {
      const data = await sendChat(originalMessage, chatSettings, [scopeId]);
      const assistant: ChatMessage =
        data?.message
          ? { ...data.message, id: data.message.id ?? createMessageId() } as ChatMessage
          : { id: createMessageId(), role: "assistant", content: "Sorry, I couldn't get a response." };
      setMessages((p) => [...p, assistant]);
      if (data.plan) setDraftPlan(data.plan);
    } catch (error) {
      console.error("Failed to send scope selection:", error);
    } finally {
      setIsTyping(false);
    }
  };

  const approvePlan = async () => {
    if (!draftPlan) return;
    try {
      await postApprovePlan(draftPlan);
      setDraftPlan(null);
      setMessages((p) => [
        ...p,
        {
          id: createMessageId(),
          role: "assistant",
          content: "The workforce has been deployed. You can track progress on the board.",
        },
      ]);
      setActiveTab("board");
    } catch (e) {
      console.error(e);
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

        {pendingScopeOptions.length > 0 && (
          <div className="flex flex-col gap-2">
            <p className="text-sm text-text-dim">Select a scope to proceed:</p>
            <div className="flex flex-wrap gap-2">
              {pendingScopeOptions.map((scope) => (
                <button
                  key={scope.id}
                  type="button"
                  onClick={() => handleScopeSelect(scope.id)}
                  className="px-3 py-1.5 text-sm border border-text-dim rounded hover:border-text hover:text-text transition-all"
                >
                  {scope.description || scope.id}
                </button>
              ))}
            </div>
          </div>
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
