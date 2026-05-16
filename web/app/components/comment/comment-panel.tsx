"use client";

import { useEffect, useRef, useState } from "react";
import { addTaskComment, fetchTaskComments } from "@/lib/api";
import { CommentItem } from "./comment-item";
import { TaskComment } from "@/lib/types";


export function CommentPanel({ taskId }: { taskId: string }) {
  const [comments, setComments] = useState<TaskComment[]>([]);
  const [text, setText] = useState("");
  const [loading, setLoading] = useState(false);

  const bottomRef = useRef<HTMLDivElement>(null);

  // ---------------- LOAD ----------------
  useEffect(() => {
    async function load() {
      if (!taskId) return;
      const data = await fetchTaskComments(taskId);
      setComments(data || []);
    }
    load();
  }, [taskId]);

  // ---------------- AUTO SCROLL ----------------
  useEffect(() => {
    bottomRef.current?.scrollIntoView({
      behavior: "smooth",
    });
  }, [comments]);

  // ---------------- SAVE COMMENT ----------------
  async function handleSave() {
    const trimmed = text.trim();
    if (!trimmed || loading) return;

    const optimistic: TaskComment = {
      id: `temp-${Date.now()}`,
      taskId,
      message: trimmed,
      createdAt: new Date().toISOString(),
      author: {
        id: "me",
        name: "You"
      },
    };

    setComments((prev) => [...prev, optimistic]);
    setText("");
    setLoading(true);

    try {
      const saved = await addTaskComment(taskId, trimmed);

      setComments((prev) =>
        prev.map((c) =>
          c.id === optimistic.id ? saved : c
        )
      );
    } catch (err) {
      console.error(err);

      setComments((prev) =>
        prev.filter((c) => c.id !== optimistic.id)
      );
    } finally {
      setLoading(false);
    }
  }

  // ---------------- CANCEL ----------------
  function handleCancel() {
    setText("");
  }

  return (
    <div className="flex flex-col h-full">

      {/* COMMENTS LIST */}
      <div className="flex-1 overflow-y-auto space-y-4 p-4">
        {comments.length === 0 ? (
          <div className="text-center text-sm text-text-dim py-10">
            No comments yet. Start the discussion.
          </div>
        ) : (
          comments.map((comment) => (
            <CommentItem
              key={comment.id}
              comment={comment}
            />
          ))
        )}

        <div ref={bottomRef} />
      </div>

      {/* COMPOSER */}
      <div className="border-t border-border p-3">
        <textarea
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder="Write a comment..."
          rows={3}
          className="w-full resize-none bg-bg border border-border rounded px-3 py-2 text-sm text-text focus:outline-none"
        />

        {/* ACTIONS */}
        <div className="flex justify-end gap-2 mt-2">
          <button
            type="button"
            onClick={handleCancel}
            disabled={!text.trim() || loading}
            className="px-3 py-2 text-sm rounded border border-border text-text-dim hover:text-text disabled:opacity-50"
          >
            Cancel
          </button>

          <button
            type="button"
            onClick={handleSave}
            disabled={!text.trim() || loading}
            className="px-4 py-2 text-sm rounded bg-blue-600 text-white disabled:opacity-50"
          >
            {loading ? "Saving..." : "Save"}
          </button>
        </div>
      </div>
    </div>
  );
}
