"use client";

import { motion, AnimatePresence } from "framer-motion";
import { X } from "lucide-react";

export interface ChatSettings {
  provider: string;
  model: string;
  effort: "low" | "medium" | "high";
}

interface ChatSettingsModalProps {
  open: boolean;
  onClose: () => void;
  settings: ChatSettings;
  setSettings: React.Dispatch<React.SetStateAction<ChatSettings>>;
  onSave: (settings: ChatSettings) => void;
}

export function ChatSettingsModal({
  open,
  onClose,
  settings,
  setSettings,
  onSave,
}: ChatSettingsModalProps) {
  
  return (
    <AnimatePresence>
      {open && (
        <>
          {/* backdrop */}
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            onClick={onClose}
            className="fixed inset-0 bg-black/40 z-40"
          />

          {/* modal */}
          <motion.div
            initial={{ opacity: 0, y: 10, scale: 0.98 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: 10, scale: 0.98 }}
            transition={{ duration: 0.18 }}
            className="fixed top-24 right-6 w-[360px] bg-panel border border-border rounded-xl shadow-2xl z-50 overflow-hidden"
          >
            {/* header */}
            <div className="flex items-center justify-between px-4 py-3 border-b border-border">
              <div>
                <h2 className="text-sm font-bold text-text">
                  Chat AI Settings
                </h2>

                <p className="text-[10px] text-text-dim mt-0.5">
                  Applies only to chat interactions
                </p>
              </div>

              <button
                onClick={onClose}
                className="text-text-dim hover:text-text transition-colors"
              >
                <X size={16} />
              </button>
            </div>

            {/* content */}
            <div className="p-4 space-y-5">
              {/* Provider */}
              <div className="space-y-1">
                <label className="text-[11px] text-text-dim uppercase tracking-wider">
                  Provider
                </label>

                <select
                  value={settings.provider}
                  onChange={(e) =>
                    setSettings(prev => ({
                      ...prev,
                      provider: e.target.value,
                    }))
                  }
                  className="w-full bg-bg border border-border rounded-md px-3 py-2 text-sm outline-none"
                >
                  <option value="openai">OpenAI</option>
                  <option value="anthropic">Anthropic</option>
                  <option value="local">Local</option>
                </select>
              </div>

              {/* Model */}
              <div className="space-y-1">
                <label className="text-[11px] text-text-dim uppercase tracking-wider">
                  Model
                </label>

                <select
                  value={settings.model}
                  onChange={(e) =>
                    setSettings(prev => ({
                      ...prev,
                      model: e.target.value,
                    }))
                  }
                  className="w-full bg-bg border border-border rounded-md px-3 py-2 text-sm outline-none"
                >
                  <option value="gpt-4o">GPT-4o</option>
                  <option value="gpt-4.1">GPT-4.1</option>
                  <option value="claude-3.5-sonnet">
                    Claude 3.5 Sonnet
                  </option>
                </select>
              </div>

              {/* Effort */}
              <div className="space-y-2">
                <label className="text-[11px] text-text-dim uppercase tracking-wider">
                  Effort
                </label>

                <div className="flex gap-2">
                  {(["low", "medium", "high"] as const).map((level) => (
                    <button
                      key={level}
                      onClick={() =>
                        setSettings(prev => ({
                          ...prev,
                          effort: level,
                        }))
                      }
                      className={`flex-1 py-2 rounded-md border text-xs font-bold uppercase tracking-wider transition-all ${
                        settings.effort === level
                          ? "bg-accent text-white border-accent"
                          : "border-border text-text-dim hover:border-text-dim"
                      }`}
                    >
                      {level}
                    </button>
                  ))}
                </div>
              </div>
            </div>

            <div className="flex justify-end gap-2 px-4 py-3 border-t border-border">
              <button
                onClick={onClose}
                className="px-4 py-2 text-xs border border-border rounded-md text-text-dim hover:bg-bg transition-all"
              >
                Cancel
              </button>

              <button
                onClick={() => {
                  onSave(settings);
                  onClose();
                }}
                className="px-4 py-2 text-xs rounded-md bg-accent text-white hover:opacity-90 transition-all"
              >
                Save Changes
              </button>
            </div>
          </motion.div>
        </>
      )}
    </AnimatePresence>
  );
}