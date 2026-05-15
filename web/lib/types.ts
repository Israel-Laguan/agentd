export enum TaskStatus {
  PENDING = 'PENDING',
  READY = 'READY',
  QUEUED = 'QUEUED',
  RUNNING = 'RUNNING',
  COMPLETED = 'COMPLETED',
  FAILED = 'FAILED',
  BLOCKED = 'BLOCKED',
  IN_CONSIDERATION = 'IN_CONSIDERATION'
}

export interface TaskLog {
  message: string;
  timestamp: number;
}

export interface Task {
  id: string;
  projectId: string;
  title: string;
  description: string;
  status: TaskStatus;
  dependsOn: string[];
  logs: TaskLog[];
  createdAt: number;
  updatedAt: number;
}

export interface Project {
  id: string;
  name: string;
  description: string;
  status: 'ACTIVE' | 'COMPLETED' | 'ARCHIVED';
  createdAt: number;
}

export interface WorkforceState {
  activeWorkers: number;
  maxWorkers: number;
  queueLength: number;
}

export interface ChatMessage {
  role: 'user' | 'assistant';
  content: string;
}

export interface ChatResponse {
  message: ChatMessage;
  plan?: DraftPlan;
}

interface DraftPlanTask {
  title: string;
  description: string;
};

export interface DraftPlan {
  name: string;
  description: string;
  tasks: DraftPlanTask[];
};
