import { Task, TaskStatus, TaskLog, TaskComment } from "@/lib/types";

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

// mapDaemonTask converts a raw daemon Task (PascalCase, no json tags) to the
// web Task shape (camelCase). Fields with no direct web equivalent are dropped
// or degraded gracefully.
export function mapDaemonTask(raw: Record<string, unknown>): Task {
  return {
    id: (raw.ID ?? raw.id ?? "") as string,
    projectId: (raw.ProjectID ?? raw.projectId ?? "") as string,
    title: (raw.Title ?? raw.title ?? "") as string,
    description: (raw.Description ?? raw.description ?? "") as string,
    // Daemon uses "State"; web uses "status". Values align: PENDING, RUNNING, etc.
    status: ((raw.State ?? raw.State ?? raw.status ?? "PENDING") as string) as TaskStatus,
    dependsOn: ((raw.DependsOn ?? raw.dependsOn ?? []) as string[]),
    // Logs is a raw string on the daemon side; degrade to empty structured array.
    logs: [] as TaskLog[],
    createdAt: isoToMs(raw.CreatedAt ?? raw.createdAt),
    updatedAt: isoToMs(raw.UpdatedAt ?? raw.updatedAt),
    token_usage: (raw.TokenUsage ?? raw.token_usage) as number | undefined,
  };
}

// mapDaemonComment converts a raw daemon Comment (PascalCase) to the web
// TaskComment shape. The daemon Comment.Body field carries the text content.
export function mapDaemonComment(raw: Record<string, unknown>): TaskComment {
  const author = (raw.Author ?? {}) as Record<string, unknown>;
  return {
    id: (raw.ID ?? raw.id ?? "") as string,
    taskId: (raw.TaskID ?? raw.taskId ?? "") as string,
    message: (raw.Body ?? raw.Content ?? raw.body ?? "") as string,
    createdAt: typeof (raw.CreatedAt ?? raw.createdAt) === "string"
      ? (raw.CreatedAt ?? raw.createdAt) as string
      : new Date(isoToMs(raw.CreatedAt ?? raw.createdAt)).toISOString(),
    author: {
      id: (author.ID ?? author.id ?? "system") as string,
      name: (author.Name ?? author.name ?? "System") as string,
    },
  };
}
