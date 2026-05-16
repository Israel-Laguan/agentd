"use client";

import { Task, TaskStatus } from "@/lib/types";
import { X } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";
import { useEffect, useRef, useState } from "react";
import { CommentPanel } from "../comment/comment-panel";

interface TaskDrawerProps {
  task: Task | null;
  onClose: () => void;
}

export function TaskDrawer({ task, onClose }: TaskDrawerProps) {
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [isEditingTitle, setIsEditingTitle] = useState(false);
  const [isEditingDescription, setIsEditingDescription] = useState(false);
  const [status, setStatus] = useState<TaskStatus>(task?.status || TaskStatus.PENDING);
  const lastTaskIdRef = useRef<string | null>(null);

  // sync when task changes
  useEffect(() => {
    if (!task) return;

    // only reset state when switching tasks
    if (lastTaskIdRef.current !== task.id) {
      setTitle(task.title);
      setDescription(task.description);
      setStatus(task.status);
      lastTaskIdRef.current = task.id;
    }
  }, [task]);

if (!task) return null;

  return (
    <AnimatePresence>
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
          <div className="p-4 space-y-5 overflow-y-auto">

            {/* TITLE (editable) */}
            <div>
              <label className="text-[10px] text-text-dim">Title</label>

              {isEditingTitle ? (
                <input
                  value={title}
                  onChange={(e) => setTitle(e.target.value)}
                  onBlur={() => setIsEditingTitle(false)}
                  autoFocus
                  className="w-full text-sm text-text bg-bg border border-border rounded px-2 py-1"
                />
              ) : (
                <p
                  className="text-sm text-text font-medium cursor-text"
                  onClick={() => setIsEditingTitle(true)}
                >
                  {title}
                </p>
              )}
            </div>

            {/* DESCRIPTION (editable) */}
            <div>
              <label className="text-[10px] text-text-dim">Description</label>

              {isEditingDescription ? (
                <textarea
                  value={description}
                  onChange={(e) => setDescription(e.target.value)}
                  onBlur={() => setIsEditingDescription(false)}
                  autoFocus
                  className="w-full text-sm text-text-dim bg-bg border border-border rounded px-2 py-1 min-h-[100px]"
                />
              ) : (
                <p
                  className="text-sm text-text-dim cursor-text whitespace-pre-wrap"
                  onClick={() => setIsEditingDescription(true)}
                >
                  {description}
                </p>
              )}
            </div>

            {/* STATUS */}
            <div>
              <label className="text-[10px] text-text-dim pr-2">Status</label>
              <select
                value={status}
                onChange={(e) => setStatus(e.target.value as TaskStatus)}
                className="mt-1 text-sm text-text bg-bg border border-border rounded px-2 py-1"
              >
                {Object.values(TaskStatus).map((s) => (
                  <option key={s} value={s}>
                    {s}
                  </option>
                ))}
              </select>
            </div>

            {/* CREATED */}
            <div>
              <label className="text-[10px] text-text-dim">Created</label>
              <p className="text-xs text-text-dim">
                {new Date(task.createdAt).toLocaleString()}
              </p>
            </div>

            {/* UPDATED */}
            <div>
              <label className="text-[10px] text-text-dim">Updated</label>
              <p className="text-xs text-text-dim">
                {new Date(task.updatedAt).toLocaleString()}
              </p>
            </div>

            {/* COMMENTS */}
            <div className="pt-4 border-t border-border">
              <CommentPanel taskId={task.id} />
            </div>
          </div>
        </motion.div>
      </>
    </AnimatePresence>
  );
}