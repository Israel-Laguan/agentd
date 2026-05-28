"use client";

import { useState, useRef, useCallback } from "react";
import {
  ChatMessage,
  ChatResponse,
  ChatScopeOption,
  ChatStatusReport,
  DraftPlan,
  MaterializeResult,
} from "@/lib/types";
import { sendChat, postApprovePlan } from "@/lib/api";
import { ChatSettings } from "./chat-settings-modal";

function createMessageId() {
  return globalThis.crypto?.randomUUID?.() ?? `msg-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

export function useChatSession(
  chatSettings: ChatSettings,
  setMessages: React.Dispatch<React.SetStateAction<ChatMessage[]>>,
  draftPlan: DraftPlan | null,
  setDraftPlan: React.Dispatch<React.SetStateAction<DraftPlan | null>>,
  setActiveTab: (tab: string) => void,
  onMaterialized?: (result: MaterializeResult) => void | Promise<void>
) {
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
      ? { ...data.message, id: data.message.id || createMessageId() } as ChatMessage
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

  const handleSend = async (input: string, setInput: (v: string) => void, setIsTyping: (v: boolean) => void) => {
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

  const handleScopeSelect = async (scopeId: string, setIsTyping: (v: boolean) => void) => {
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

  const sendSuggested = async (text: string, setIsTyping: (v: boolean) => void) => {
    lastUserMessageRef.current = text;
    const userMsg = { id: createMessageId(), role: "user" as const, content: text };
    setMessages((p) => [...p, userMsg]);
    setIsTyping(true);
    clearStructuredUi();

    try {
      await runChat(text);
    } catch (error) {
      console.error("Failed to send suggested message:", error);
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
    if (!draftPlan) return;
    let result: MaterializeResult;
    try {
      result = await postApprovePlan(draftPlan);
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
      return;
    }
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
    try {
      await onMaterialized?.(result);
    } catch (e) {
      console.error("onMaterialized callback error:", e);
    }
    setActiveTab("board");
  };

  return {
    pendingScopeOptions,
    showIntentActions,
    statusReport,
    clearStructuredUi,
    handleSend,
    handleScopeSelect,
    sendSuggested,
    approvePlan,
  };
}
