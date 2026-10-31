import { motion } from "framer-motion";
import { cn } from "@/lib/utils";
import { TaskStatus } from "@/lib/types";
import { formatTokenCount } from "@/lib/format";
import {
  AlertCircle,
  CheckCircle2,
  Clock,
  GripVertical,
  HelpCircle,
  Loader2,
  MessageSquare,
  Play,
  User,
  Zap,
} from "lucide-react";
import { useDraggable } from "@dnd-kit/core";
import { CSS } from "@dnd-kit/utilities";

// A privileged-command handoff ends with the daemon's detection payload:
// pattern=<p> command=<Go %q string> exit=<n> duration=<d>, then the command's
// output (internal/queue/worker/worker_addons.go detectionPayload).
const HANDOFF_PAYLOAD = /(?:^|\n)pattern=\S+ command="((?:[^"\\]|\\.)*)"/;

// splitHandoffCommand returns the description's text before a handoff payload
// and the payload's command, unquoted. The output after it is left to the task
// drawer. A description without a payload comes back unchanged.
export function splitHandoffCommand(description: string): { text: string; command?: string } {
  const match = HANDOFF_PAYLOAD.exec(description);
  if (!match) return { text: description };
  let command = match[1];
  try {
    // Go's %q escapes match JSON's for quotes, backslashes and common controls.
    command = JSON.parse(`"${match[1]}"`);
  } catch {
    // An escape JSON lacks (\x, \U): show the command as Go wrote it.
  }
  return { text: description.slice(0, match.index).trim(), command };
}

interface TaskCardProps {
  task: {
    id: string;
    title: string;
    description: string;
    state: TaskStatus;
    assignee: "HUMAN" | "SYSTEM";
    token_usage?: number;
  };
  onClick: () => void;
}

export const TaskCard = ({ task, onClick }: TaskCardProps) => {
  const statusColors = {
    [TaskStatus.PENDING]: "bg-border/20 text-text-dim border-border",
    [TaskStatus.READY]: "bg-green-500/10 text-green-400 border-green-500/20",
    [TaskStatus.QUEUED]: "bg-blue/10 text-blue border-blue/20",
    [TaskStatus.RUNNING]: "bg-blue/10 text-blue border-blue shadow-[0_0_10px_rgba(88,166,255,0.15)]",
    [TaskStatus.COMPLETED]: "bg-accent/10 text-accent border-accent/20",
    [TaskStatus.FAILED]: "bg-error/10 text-error border-error/20",
    [TaskStatus.FAILED_REQUIRES_HUMAN]: "bg-error/20 text-error border-error/40",
    [TaskStatus.NEEDS_CONTEXT]: "bg-warning/10 text-warning border-warning/20",
    [TaskStatus.BLOCKED]: "bg-warning/10 text-warning border-warning/20",
    [TaskStatus.IN_CONSIDERATION]: "bg-purple-500/10 text-purple-400 border-purple-500/20",
  };

  const Icons = {
    [TaskStatus.PENDING]: Clock,
    [TaskStatus.READY]: Zap,
    [TaskStatus.QUEUED]: Loader2,
    [TaskStatus.RUNNING]: Play,
    [TaskStatus.COMPLETED]: CheckCircle2,
    [TaskStatus.FAILED]: AlertCircle,
    [TaskStatus.FAILED_REQUIRES_HUMAN]: User,
    [TaskStatus.NEEDS_CONTEXT]: HelpCircle,
    [TaskStatus.BLOCKED]: AlertCircle,
    [TaskStatus.IN_CONSIDERATION]: MessageSquare,
  };

  const StatusIcon = Icons[task.state as TaskStatus] || AlertCircle;
  const { text, command } = splitHandoffCommand(task.description ?? "");
  const {
    attributes,
    listeners,
    setNodeRef,
    setActivatorNodeRef,
    transform,
    isDragging,
  } = useDraggable({
    id: task.id,
  });
  const style = {
    transform: CSS.Translate.toString(transform),
  };

  return (
    <motion.div
      ref={setNodeRef}
      style={style}
      layout
      className={cn(
        "p-3 bg-panel border border-border rounded-md shadow-sm hover:border-text-dim/30 transition-all cursor-pointer group",
        isDragging && "opacity-50 ring-2 ring-blue z-50"
      )}
      onClick={onClick}
    >
      <div className="flex justify-between items-start mb-2 gap-2">
       <div className="flex items-center gap-2">
        <button
          type="button"
          aria-label="Drag task"
          title="Drag task"
          ref={setActivatorNodeRef}
          {...listeners}
          {...attributes}
          onClick={(e) => e.stopPropagation()}
          className="cursor-grab active:cursor-grabbing text-text-dim hover:text-text transition-colors"
        >
          <GripVertical size={16} />
        </button>

        <h4 className="text-[13px] font-medium text-text leading-tight group-hover:text-blue transition-colors">
          {task.title}
        </h4>
      </div>

        <div className={cn("shrink-0 flex items-center gap-1 px-1.5 py-0.5 rounded text-[9px] font-bold uppercase tracking-wider border", statusColors[task.state as TaskStatus])}>
          <StatusIcon size={10} className={task.state === TaskStatus.RUNNING ? "animate-spin" : ""} />
          {task.state}
        </div>
      </div>
      {task.assignee === "HUMAN" && (
        <div className="mb-2">
          <span className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[9px] font-bold uppercase tracking-wider border border-purple-500/20 bg-purple-500/10 text-purple-400">
            <User size={10} />
            Human action
          </span>
        </div>
      )}
      {text && (
        <p className="mb-2 text-[11px] text-text-dim leading-relaxed line-clamp-3 break-words whitespace-pre-line">{text}</p>
      )}
      {command && (
        <code title={command} className="block mb-2 truncate bg-bg/80 px-1 py-0.5 rounded font-mono text-[10px] text-text">
          {command}
        </code>
      )}
      <div className="flex items-center gap-2">
        {(task.token_usage ?? 0) > 0 && (
          <span className="text-[9px] font-mono text-text-dim shrink-0" title="Tokens used">
            {formatTokenCount(task.token_usage!)} tok
          </span>
        )}
         <div className="h-1 flex-1 bg-border/30 rounded-full overflow-hidden">
            <div
              className={cn(
                "h-full transition-all duration-700",
                task.state === TaskStatus.COMPLETED ? "w-full bg-accent" :
                task.state === TaskStatus.RUNNING ? "w-2/3 bg-blue animate-pulse" :
                task.state === TaskStatus.READY ? "w-1/3 bg-green-500/60" : "w-0"
              )}
            />
         </div>
         <span className="text-[9px] text-text-dim/50 font-mono">#{task.id.slice(0, 4)}</span>
      </div>
    </motion.div>
  );
};
