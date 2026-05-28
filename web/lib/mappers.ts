import { Task, TaskStatus, TaskLog, TaskComment, DraftPlan, DraftPlanTask } from "@/lib/types";

// unwrapData extracts the `.data` field from the daemon's standard envelope
// { status, data, meta?, error? }. Used by all real-API fetch functions.
export function unwrapData<T>(envelope: unknown): T {
  return (envelope as { data: T }).data;
}

// isoToMs converts an ISO 8601 timestamp string (time.Time Go output) to a
// Unix millisecond number, matching the number type used by the web Task type.
function isoToMs(value: unknown): number {
  if (typeof value === "number") return value;
  if (typeof value === "string") return new Date(value).getTime();
  return 0;
}

function authorFromDaemon(raw: unknown): { id: string; name: string } {
  if (typeof raw === "string") {
    const upper = raw.toUpperCase();
    switch (upper) {
      case "USER":
      case "HUMAN":
        return { id: raw, name: "You" };
      case "FRONTDESK":
        return { id: raw, name: "Frontdesk" };
      case "WORKER_AGENT":
      case "SYSTEM":
        return { id: raw, name: "Agent" };
      default:
        return { id: raw, name: raw };
    }
  }
  if (raw && typeof raw === "object") {
    const authorObj = raw as Record<string, unknown>;
    return {
      id: (authorObj.ID ?? authorObj.id ?? "system") as string,
      name: (authorObj.Name ?? authorObj.name ?? "System") as string,
    };
  }
  return { id: "system", name: "System" };
}

// mapDaemonTask converts a raw daemon Task (snake_case with PascalCase fallback) to the
// web Task shape. Fields with no direct web equivalent are dropped
// or degraded gracefully.
export function mapDaemonTask(raw: Record<string, unknown>): Task {
  return {
    id: (raw.ID ?? raw.id ?? "") as string,
    project_id: (raw.ProjectID ?? raw.project_id ?? raw.projectId ?? "") as string,
    title: (raw.Title ?? raw.title ?? "") as string,
    description: (raw.Description ?? raw.description ?? "") as string,
    state: ((raw.State ?? raw.state ?? raw.status ?? "PENDING") as string) as TaskStatus,
    depends_on: ((raw.DependsOn ?? raw.depends_on ?? raw.dependsOn ?? []) as string[]),
    // Logs is a raw string on the daemon side; degrade to empty structured array.
    logs: [] as TaskLog[],
    created_at: isoToMs(raw.CreatedAt ?? raw.created_at ?? raw.createdAt),
    updated_at: isoToMs(raw.UpdatedAt ?? raw.updated_at ?? raw.updatedAt),
    token_usage: (raw.TokenUsage ?? raw.token_usage) as number | undefined,
  };
}

// mapDaemonDraftPlan converts a raw daemon DraftPlan (project_name / PascalCase
// fallback) to the web DraftPlan shape. project_name maps to name; task
// ref_id / temp_id map to the optional DraftPlanTask.id.
export function mapDaemonDraftPlan(raw: Record<string, unknown> | null | undefined): DraftPlan {
  const safeRaw = raw ?? {};
  const name = (safeRaw.project_name ?? safeRaw.ProjectName ?? "") as string;
  const description = (safeRaw.description ?? safeRaw.Description ?? "") as string;
  const rawTasksValue = safeRaw.tasks ?? safeRaw.Tasks;
  const rawTasks = Array.isArray(rawTasksValue) ? (rawTasksValue as Record<string, unknown>[]) : [];
  const tasks: DraftPlanTask[] = rawTasks.map((t) => {
    const safeT = t ?? {};
    const rawID = safeT.ref_id ?? safeT.ReferenceID ?? safeT.temp_id ?? safeT.TempID;
    const id = typeof rawID === "string" ? rawID.trim() || undefined : undefined;
    return {
      ...(id !== undefined ? { id } : {}),
      title: (safeT.title ?? safeT.Title ?? "") as string,
      description: (safeT.description ?? safeT.Description ?? "") as string,
    };
  });
  return { name, description, tasks };
}

// mapDaemonComment converts a raw daemon Comment (PascalCase) to the web
// TaskComment shape. The daemon Comment.Body field carries the text content.
export function mapDaemonComment(raw: Record<string, unknown>): TaskComment {
  const author = authorFromDaemon(raw.Author ?? raw.author);

  const rawCreatedAt = raw.CreatedAt ?? raw.created_at ?? raw.createdAt;
  const createdAt = typeof rawCreatedAt === "string"
    ? rawCreatedAt
    : new Date(isoToMs(rawCreatedAt)).toISOString();

  return {
    id: (raw.ID ?? raw.id ?? "") as string,
    task_id: (raw.TaskID ?? raw.task_id ?? raw.taskId ?? "") as string,
    message: (raw.Body ?? raw.Content ?? raw.body ?? raw.content ?? "") as string,
    created_at: createdAt,
    author,
  };
}
