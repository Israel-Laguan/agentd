import { mockChat } from "@/lib/mocks/chat.mock";
import {
  ChatResponse,
  DraftPlan,
  MaterializeResult,
} from "@/lib/types";
import { unwrapData, mapDaemonDraftPlan } from "@/lib/mappers";
import { parseStructuredIntake, displayContentForChat } from "@/lib/chat-intake";
import { API, CREATE_PLAN_TOOL_DEF, MATERIALIZE_TOKEN, USE_MOCK, ChatSettings } from "@/lib/api-config";

export async function sendChat(
  message: string,
  settings?: ChatSettings,
  approvedScopes?: string[]
): Promise<ChatResponse> {
  if (USE_MOCK) return mockChat(message);

  const body: Record<string, unknown> = {
    model: settings?.model,
    messages: [{ role: "user", content: message }],
    tools: [CREATE_PLAN_TOOL_DEF],
  };
  if (approvedScopes && approvedScopes.length > 0) {
    body.approved_scopes = approvedScopes;
  }

  const res = await fetch(`${API}/v1/chat/completions`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!res.ok) throw new Error(`HTTP error! status: ${res.status}`);
  const envelope = await res.json();
  const choice = envelope?.choices?.[0];
  const content: string | null | undefined = choice?.message?.content;
  const toolCalls: { function?: { name?: string; arguments?: string } }[] | undefined =
    choice?.message?.tool_calls;

  if (!content && (!toolCalls || toolCalls.length === 0)) {
    throw new Error("Invalid chat response: missing choices[0].message.content and tool_calls");
  }

  let plan: DraftPlan | undefined;
  if (toolCalls && toolCalls.length > 0) {
    const createPlanCall = toolCalls.find((tc) => tc.function?.name === "create_plan");
    if (createPlanCall?.function?.arguments) {
      try {
        plan = mapDaemonDraftPlan(JSON.parse(createPlanCall.function.arguments));
      } catch {
        // malformed arguments — fall through to content fallback
      }
    }
  }
  if (!plan && content) {
    try {
      const parsed = JSON.parse(content) as Record<string, unknown>;
      if (parsed.project_name || parsed.ProjectName || parsed.tasks || parsed.Tasks) {
        plan = mapDaemonDraftPlan(parsed);
      }
    } catch {
      // plain text content — no plan
    }
  }

  const intake = parseStructuredIntake(content, toolCalls);
  const displayContent = displayContentForChat(content, Boolean(plan), intake);

  return {
    message: {
      id: envelope?.id || `msg-${Date.now()}-${Math.random().toString(16).slice(2)}`,
      role: "assistant" as const,
      content: displayContent,
    },
    ...(plan ? { plan } : {}),
    ...(intake?.statusReport ? { statusReport: intake.statusReport } : {}),
    ...(intake?.scopeClarification
      ? { scopeClarification: intake.scopeClarification }
      : {}),
    ...(intake?.intentClarification
      ? { intentClarification: intake.intentClarification }
      : {}),
  } satisfies ChatResponse;
}

export async function postApprovePlan(plan: DraftPlan): Promise<MaterializeResult> {
  if (USE_MOCK) {
    return {
      projectId: "mock-project",
      taskIds: plan.tasks.map((t, i) => t.id ?? `mock-task-${i}`),
    };
  }

  const daemonBody = {
    project_name: plan.name,
    description: plan.description,
    tasks: plan.tasks.map((t) => ({
      title: t.title,
      description: t.description,
      ...(t.id ? { temp_id: t.id } : {}),
    })),
  };

  const headers: Record<string, string> = { "Content-Type": "application/json" };
  if (MATERIALIZE_TOKEN) {
    headers["X-Agentd-Materialize-Token"] = MATERIALIZE_TOKEN;
  }

  const res = await fetch(`${API}/api/v1/projects/materialize`, {
    method: "POST",
    headers,
    body: JSON.stringify(daemonBody),
  });

  if (!res.ok) {
    throw new Error(`Failed to approve plan: ${res.status}`);
  }

  const data = unwrapData<Record<string, unknown>>(await res.json());
  const project = (data?.project ?? data?.Project) as Record<string, unknown> | undefined;
  const projectId = String(
    project?.id ??
      project?.ID ??
      data?.project_id ??
      data?.projectId ??
      data?.id ??
      data?.ID ??
      ""
  );
  const taskIds = ((data?.tasks as Record<string, unknown>[] | undefined) ?? [])
    .map((t) => String(t.id ?? t.ID ?? ""))
    .filter(Boolean);
  if (!projectId) {
    throw new Error("Failed to approve plan: missing project id in response");
  }
  return { projectId, taskIds };
}
