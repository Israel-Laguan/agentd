import { TaskComment } from "@/lib/types";
import Image from "next/image";

interface CommentItemProps {
    comment: TaskComment;
}

export const CommentItem = ({ comment }: CommentItemProps) => {
  return (
    <div className="flex gap-3">

      {/* content */}
      <div className="flex-1">
        <div className="flex items-center gap-2">
          <span className="font-medium text-sm">
            {comment.author.name}
          </span>

          <span className="text-xs text-gray-400">
            {new Date(comment.createdAt).toLocaleString()}
          </span>
        </div>

        <div className="text-sm mt-1 whitespace-pre-wrap">
          {comment.message}
        </div>
      </div>
    </div>
  );
}