"use client";

import { useEffect, useState } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { X } from "lucide-react";
import { fetchProviders } from "@/lib/api";
import { Provider } from "@/lib/types";

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
const PROVIDER_MODELS_FALLBACK: Provider[] = [
  { name: "openai", adapter: "openai", models: ["gpt-4o", "gpt-4.1"] },
  { name: "anthropic", adapter: "anthropic", models: ["claude-3.5-sonnet"] },
  { name: "local", adapter: "ollama", models: ["llama-3", "mistral"] },
];

function reconcileChatSettings(
  resolved: Provider[],
  prev: ChatSettings
): ChatSettings {
  const matched = resolved.find((p) => p.name === prev.provider);
  if (matched) {
    const model = matched.models.includes(prev.model)
      ? prev.model
      : (matched.models[0] ?? "");
    return { ...prev, model };
  }
  const first = resolved[0];
  return {
    ...prev,
    provider: first?.name ?? "",
    model: first?.models[0] ?? "",
  };
}

export function ChatSettingsModal({
  open,
  onClose,
  settings,
  setSettings,
  onSave,
}: ChatSettingsModalProps) {
  const [providers, setProviders] = useState<Provider[]>([]);
  const [fetchError, setFetchError] = useState(false);

  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    fetchProviders()
      .then((list) => {
        if (cancelled) return;
        const resolved = list.length > 0 ? list : PROVIDER_MODELS_FALLBACK;
        setProviders(resolved);
        setSettings((prev) => reconcileChatSettings(resolved, prev));
        setFetchError(false);
      })
      .catch(() => {
        if (cancelled) return;
        const resolved = PROVIDER_MODELS_FALLBACK;
        setProviders((prev) => (prev.length > 0 ? prev : resolved));
        setSettings((prev) => reconcileChatSettings(resolved, prev));
        setFetchError(true);
      });
    return () => {
      cancelled = true;
    };
  }, [open, setSettings]);

  const isLoading = open && providers.length === 0 && !fetchError;

  const activeProvider = providers.find((p) => p.name === settings.provider);
  const availableModels = activeProvider?.models ?? [];

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
              {fetchError && (
                <p className="text-[11px] text-red-400">Could not reach daemon — showing defaults.</p>
              )}

              {/* Provider */}
              <div className="space-y-1">
                <label htmlFor="provider-select" className="text-[11px] text-text-dim uppercase tracking-wider">
                  Provider
                </label>

                <select
                  id="provider-select"
                  value={settings.provider}
                  disabled={isLoading}
                  onChange={(e) => {
                    const providerName = e.target.value;
                    const providerEntry = providers.find((p) => p.name === providerName);
                    setSettings(prev => ({
                      ...prev,
                      provider: providerName,
                      model: providerEntry?.models[0] ?? "",
                    }));
                  }}
                  className="w-full bg-bg border border-border rounded-md px-3 py-2 text-sm outline-none disabled:opacity-50"
                >
                  {isLoading ? (
                    <option value="">Loading...</option>
                  ) : (
                    providers.map((p) => (
                      <option key={p.name} value={p.name}>{p.name}</option>
                    ))
                  )}
                </select>
              </div>

              {/* Model */}
              <div className="space-y-1">
                <label htmlFor="model-select" className="text-[11px] text-text-dim uppercase tracking-wider">
                  Model
                </label>

                <select
                  id="model-select"
                  value={settings.model}
                  onChange={(e) =>
                    setSettings(prev => ({
                      ...prev,
                      model: e.target.value,
                    }))
                  }
                  className="w-full bg-bg border border-border rounded-md px-3 py-2 text-sm outline-none"
                >
                  {availableModels.map((model) => (
                    <option
                      key={model}
                      value={model}
                    >
                      {model}
                    </option>
                  ))}
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