import { BreakerInfo, SystemStatus } from "@/lib/types";

const stateLabel = (state: string) => state.toLowerCase().replace("_", "-");

function detail(name: string, b: BreakerInfo) {
  const last = b.last_error ? `, last error: ${b.last_error}` : "";
  return `${name}: ${b.state}, ${b.failure_count} failures${last}`;
}

// BreakerBadge shows every circuit breaker that is not CLOSED. While one is
// open the daemon stops dispatching, so work stalls even though the daemon
// itself answers; without this badge the board looks healthy.
export function BreakerBadge({ status }: { status: SystemStatus | null }) {
  if (!status) return null;
  const tripped: [string, string, BreakerInfo][] = [];
  if (status.breaker && status.breaker.state !== "CLOSED") {
    tripped.push(["Breaker", "dispatch", status.breaker]);
  }
  const providers = Object.entries(status.provider_breakers ?? {}).sort(([a], [b]) => a.localeCompare(b));
  for (const [name, b] of providers) {
    if (b.state !== "CLOSED") tripped.push([name, name, b]);
  }
  if (tripped.length === 0) return null;

  const open = tripped.some(([, , b]) => b.state === "OPEN");
  return (
    <span
      role="alert"
      title={tripped.map(([, name, b]) => detail(name, b)).join("\n")}
      className={`px-2 py-0.5 rounded border text-[10px] font-bold uppercase tracking-wider ${
        open ? "bg-error/10 border-error/30 text-error" : "bg-warning/10 border-warning/30 text-warning"
      }`}
    >
      {tripped.map(([label, , b]) => `${label} ${stateLabel(b.state)}`).join(" · ")}
    </span>
  );
}
