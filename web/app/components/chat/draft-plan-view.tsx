import { CheckCircle2 } from "lucide-react";
import { DraftPlan } from "@/lib/types";
import { motion } from "framer-motion";

export function DraftPlanView({
  plan,
  onApprove,
  onReplan,
}: {
  plan: DraftPlan;
  onApprove: () => void;
  onReplan: () => void;
}) {
  return (
    <motion.div
      initial={{ y: 20, opacity: 0 }}
      animate={{ y: 0, opacity: 1 }}
      className="ml-10 p-5 bg-panel border border-accent/30 rounded-xl shadow-2xl relative overflow-hidden"
    >
      <div className="absolute top-0 left-0 w-full h-[2px] bg-accent" />
      <div className="flex items-center justify-between mb-4">
        <h3 className="font-bold text-text flex items-center gap-2 text-sm uppercase tracking-wider">
          <CheckCircle2 size={16} className="text-accent" />
          Workforce Allocation Plan
        </h3>
      </div>

      <div className="mb-4 bg-bg/50 p-3 rounded border border-border">
        <h4 className="text-xs font-bold text-blue mb-1">{plan.name}</h4>
        <p className="text-[11px] text-text-dim leading-normal">{plan.description}</p>
      </div>

      <div className="space-y-1.5 mb-5">
        {plan.tasks.map((t, i: number) => (
          <div key={t.id ?? i} className="flex gap-3 p-2.5 bg-bg/30 border border-border rounded-lg group hover:border-text-dim/20 transition-colors">
            <span className="text-[10px] font-mono flex items-center justify-center text-text-dim group-hover:text-accent">{String(i + 1).padStart(2, '0')}</span>
            <div>
              <h5 className="text-[11px] font-bold text-text uppercase tracking-tight">{t.title}</h5>
              <p className="text-[10px] text-text-dim leading-tight">{t.description}</p>
            </div>
          </div>
        ))}
      </div>

      <div className="flex gap-2">
        <button onClick={onApprove} className="flex-1 py-2 bg-accent text-white text-[11px] font-bold rounded hover:bg-accent-hover transition-all uppercase tracking-widest shadow-lg shadow-accent/10">
          EXECUTE STRATEGY
        </button>
        <button onClick={onReplan} className="px-4 py-2 border border-border text-text-dim text-[11px] font-bold rounded hover:bg-bg transition-all uppercase">
          REPLAN
        </button>
      </div>
    </motion.div>
  );
}
