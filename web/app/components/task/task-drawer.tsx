"use client";

import { Task } from "@/lib/types";
import { X } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";

interface TaskDrawerProps {
  task: Task | null;
  onClose: () => void;
}

export function TaskDrawer({ task, onClose }: TaskDrawerProps) {
  return (
    <AnimatePresence>
      {task && (
        <>
          {/* backdrop */}
          <div
            className="fixed inset-0 bg-black/40 z-40"
            onClick={onClose}
          />

          {/* drawer */}
          <motion.div
            initial={{ x: 400 }}
            animate={{ x: 0 }}
            exit={{ x: 400 }}
            transition={{ type: "spring", stiffness: 260, damping: 25 }}
            className="fixed right-0 top-0 h-full w-[420px] bg-panel border-l border-border z-50 shadow-2xl flex flex-col"
          >
            {/* header */}
            <div className="p-4 border-b border-border flex justify-between items-center">
              <h2 className="text-sm font-bold text-text">Task Details</h2>
              <button onClick={onClose}>
                <X size={16} />
              </button>
            </div>

            {/* content */}
            <div className="p-4 space-y-4 overflow-y-auto">
              <div>
                <label className="text-[10px] text-text-dim">Title</label>
                <p className="text-sm text-text font-medium">{task.title}</p>
              </div>

              <div>
                <label className="text-[10px] text-text-dim">Description</label>
                <p className="text-sm text-text-dim">{task.description}</p>
              </div>

              <div>
                <label className="text-[10px] text-text-dim">Status</label>
                <p className="text-sm text-blue font-mono">{task.status}</p>
              </div>

              <div>
                <label className="text-[10px] text-text-dim">Created</label>
                <p className="text-xs text-text-dim">
                  {new Date(task.createdAt).toLocaleString()}
                </p>
              </div>

              <div>
                <label className="text-[10px] text-text-dim">Updated</label>
                <p className="text-xs text-text-dim">
                  {new Date(task.updatedAt).toLocaleString()}
                </p>
              </div>
            </div>
          </motion.div>
        </>
      )}
    </AnimatePresence>
  );
}