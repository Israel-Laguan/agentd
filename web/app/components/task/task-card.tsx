import { motion } from "framer-motion";
import { cn } from "@/lib/utils";
import { TaskStatus } from "@/lib/types";
import {
  AlertCircle,
  CheckCircle2,
  Clock,
  GripVertical,
  Loader2,
  MessageSquare,
  Play,
  User,
  Zap,
} from "lucide-react";
import { useDraggable } from "@dnd-kit/core";
import { CSS } from "@dnd-kit/utilities";

interface TaskCardProps {
  task: {
    id: string;
    title: string;
    description: string;
    status: TaskStatus;
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
    [TaskStatus.BLOCKED]: AlertCircle,
    [TaskStatus.IN_CONSIDERATION]: MessageSquare,
  };

  const StatusIcon = Icons[task.status as TaskStatus] || AlertCircle;
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

        <div className={cn("shrink-0 flex items-center gap-1 px-1.5 py-0.5 rounded text-[9px] font-bold uppercase tracking-wider border", statusColors[task.status as TaskStatus])}>
          <StatusIcon size={10} className={task.status === TaskStatus.RUNNING ? "animate-spin" : ""} />
          {task.status}
        </div>
      </div>
      <p className="text-[11px] text-text-dim line-clamp-2 mb-3 leading-relaxed">{task.description}</p>
      <div className="flex items-center gap-2">
         <div className="h-1 flex-1 bg-border/30 rounded-full overflow-hidden">
            <div
              className={cn(
                "h-full transition-all duration-700",
                task.status === TaskStatus.COMPLETED ? "w-full bg-accent" :
                task.status === TaskStatus.RUNNING ? "w-2/3 bg-blue animate-pulse" :
                task.status === TaskStatus.READY ? "w-1/3 bg-green-500/60" : "w-0"
              )}
            />
         </div>
         <span className="text-[9px] text-text-dim/50 font-mono">#{task.id.slice(0, 4)}</span>
      </div>
    </motion.div>
  );
};
