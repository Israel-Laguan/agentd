import {
  ChatIntentClarification,
  ChatScopeClarification,
  ChatScopeOption,
  ChatStatusReport,
} from "@/lib/types";

export interface ParsedStructuredIntake {
  statusReport?: ChatStatusReport;
  scopeClarification?: ChatScopeClarification;
  intentClarification?: ChatIntentClarification;
  /** feasibility_clarification — message only, no follow-up actions */
  feasibilityMessage?: string;
}

type ToolCall = { function?: { name?: string; arguments?: string } };

function asRecord(value: unknown): Record<string, unknown> | undefined {
  return value && typeof value === "object" ? (value as Record<string, unknown>) : undefined;
}

export function mapScopeOptions(scopes: unknown): ChatScopeOption[] {
  if (!Array.isArray(scopes)) return [];
  return scopes
    .map((entry) => {
      const row = asRecord(entry);
      if (!row) return null;
      const id = String(row.id ?? row.ID ?? "").trim();
      if (!id) return null;
      const label = String(
        row.label ?? row.Label ?? row.description ?? row.Description ?? id
      ).trim() || id;
      return { id, label };
    })
    .filter((s): s is ChatScopeOption => s !== null);
}

export function mapStatusReport(raw: Record<string, unknown>): ChatStatusReport {
  const summary = asRecord(raw.summary ?? raw.Summary);
  const tasksByState = asRecord(summary?.tasks_by_state ?? summary?.TasksByState) ?? {};
  const normalized: Record<string, number> = {};
  for (const [state, count] of Object.entries(tasksByState)) {
    normalized[state] = Number(count) || 0;
  }
  return {
    message: String(raw.message ?? raw.Message ?? ""),
    totalProjects: Number(summary?.total_projects ?? summary?.TotalProjects) || 0,
    tasksByState: normalized,
  };
}

export function intakeFromRecord(raw: Record<string, unknown>): ParsedStructuredIntake | undefined {
  const kind = String(raw.kind ?? raw.Kind ?? "");
  switch (kind) {
    case "status_report":
      return { statusReport: mapStatusReport(raw) };
    case "scope_clarification":
      return {
        scopeClarification: {
          message: String(raw.message ?? raw.Message ?? ""),
          scopes: mapScopeOptions(raw.scopes ?? raw.Scopes),
        },
      };
    case "intent_clarification":
      return {
        intentClarification: {
          message: String(raw.message ?? raw.Message ?? ""),
        },
      };
    case "feasibility_clarification":
      return {
        feasibilityMessage: String(raw.message ?? raw.Message ?? ""),
      };
    default:
      return undefined;
  }
}

function intakeFromJsonString(json: string): ParsedStructuredIntake | undefined {
  try {
    const parsed = JSON.parse(json) as Record<string, unknown>;
    return intakeFromRecord(parsed);
  } catch {
    return undefined;
  }
}

export function parseStructuredIntake(
  content: string | null | undefined,
  toolCalls?: ToolCall[]
): ParsedStructuredIntake | undefined {
  if (toolCalls) {
    const statusCall = toolCalls.find((tc) => tc.function?.name === "status_report");
    if (statusCall?.function?.arguments) {
      const fromTool = intakeFromJsonString(statusCall.function.arguments);
      if (fromTool) return fromTool;
    }
  }
  if (content?.trim()) {
    return intakeFromJsonString(content);
  }
  return undefined;
}

export function displayContentForChat(
  content: string | null | undefined,
  plan: boolean,
  intake?: ParsedStructuredIntake
): string {
  if (intake?.statusReport?.message) return intake.statusReport.message;
  if (intake?.scopeClarification?.message) return intake.scopeClarification.message;
  if (intake?.intentClarification?.message) return intake.intentClarification.message;
  if (intake?.feasibilityMessage) return intake.feasibilityMessage;
  const trimmed = (content ?? "").trim();
  if (trimmed) return trimmed;
  if (plan) return "I've prepared a draft plan for your review below.";
  return "";
}
