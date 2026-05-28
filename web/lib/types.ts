export enum TaskStatus {
  PENDING = 'PENDING',
  READY = 'READY',
  QUEUED = 'QUEUED',
  RUNNING = 'RUNNING',
  COMPLETED = 'COMPLETED',
  FAILED = 'FAILED',
  FAILED_REQUIRES_HUMAN = 'FAILED_REQUIRES_HUMAN',
  BLOCKED = 'BLOCKED',
  IN_CONSIDERATION = 'IN_CONSIDERATION'
}

export interface TaskLog {
  message: string;
  timestamp: number;
}

export interface Task {
  id: string;
  project_id: string;
  title: string;
  description: string;
  state: TaskStatus;
  depends_on: string[];
  logs: TaskLog[];
  created_at: number;
  updated_at: number;
  token_usage?: number;
}

export interface Project {
  id: string;
  name: string;
  description: string;
  status: 'ACTIVE' | 'COMPLETED' | 'ARCHIVED';
  created_at: number;
}

export interface WorkforceState {
  active_workers: number;
  max_workers: number;
  queue_length: number;
}

export interface SystemStatusSummary {
  total_projects: number;
  tasks_by_state: Record<string, number>;
}

export interface SystemStatusReport {
  kind: string;
  message: string;
  summary: SystemStatusSummary;
}

export interface SystemStatus {
  total_token_usage: number;
  rolling_budget_enabled?: boolean;
  rolling_token_limit?: number;
  rolling_token_remaining?: number;
  rolling_token_window?: string;
  status?: SystemStatusReport;
}

export interface ChatMessage {
  id: string;
  role: 'user' | 'assistant';
  content: string;
}

export interface ChatStatusReport {
  message: string;
  totalProjects: number;
  tasksByState: Record<string, number>;
}

export interface ChatScopeOption {
  id: string;
  label: string;
}

export interface ChatScopeClarification {
  message: string;
  scopes: ChatScopeOption[];
}

export interface ChatIntentClarification {
  message: string;
}

export interface ChatResponse {
  message: ChatMessage;
  plan?: DraftPlan;
  statusReport?: ChatStatusReport;
  scopeClarification?: ChatScopeClarification;
  intentClarification?: ChatIntentClarification;
}

export interface MaterializeResult {
  projectId: string;
  taskIds: string[];
}

export interface DraftPlanTask {
  id?: string;
  title: string;
  description: string;
};

export interface DraftPlan {
  name: string;
  description: string;
  tasks: DraftPlanTask[];
};

export interface TaskComment {
  id: string;
  task_id: string;
  message: string;
  created_at: string;
  author: {
    id: string;
    name: string;
  };
};

export interface Provider {
  name: string;
  adapter: string;
  models: string[];
}
