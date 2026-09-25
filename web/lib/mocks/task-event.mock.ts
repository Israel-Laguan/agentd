import { TaskEvent } from "@/lib/types";

export const mockTaskEvents: TaskEvent[] = [
  {
    id: "event-result-t3",
    project_id: "p1",
    task_id: "t3",
    type: "RESULT",
    payload: "Planner implementation completed and verified.",
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  },
  {
    id: "event-result-t5",
    project_id: "p1",
    task_id: "t5",
    type: "RESULT",
    payload: "Retry exhausted after the configured limit.",
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  },
  {
    id: "event-handoff-t6",
    project_id: "p1",
    task_id: "t6",
    type: "PERMISSION_HANDOFF",
    payload: 'pattern=privilege command="sudo dmidecode --string system-serial-number" exit=1',
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  },
];
