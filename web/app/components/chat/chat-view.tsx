"use client";

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
}: ChatViewProps) {

  const handleSend = async () => {
    if (!input.trim()) return;

    const userMsg = { id: createMessageId(), role: "user", content: input } as ChatMessage;

    setMessages((p) => [...p, userMsg]);
    setInput("");
    setIsTyping(true);

    try {
      const data = await sendChat(userMsg.content);

      const assistant =
        data?.message
          ? { ...data.message, id: data.message.id ?? createMessageId() }
          : data?.choices?.[0]?.message?.content
          ? {
              id: createMessageId(),
              role: "assistant",
              content: data.choices[0].message.content,
            }
          : null;

      if (assistant) setMessages((p) => [...p, assistant]);
      if (data.plan) setDraftPlan(data.plan);
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

  const approvePlan = async () => {
    try {
      await postApprovePlan();
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
      {/* Messages */}
      <div className="flex-1 space-y-6 mb-8 overflow-y-auto pb-4 px-2">
        {messages.length === 0 && (
          <ChatEmptyState setInput={setInput} />
        )}

        {messages.map((m, i) => (
          <ChatMessageView key={m.id ?? i} message={m} />
        ))}

        {isTyping && <ChatTyping />}

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
    </motion.div>
  );
}
