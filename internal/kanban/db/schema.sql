PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS projects (
    id TEXT PRIMARY KEY NOT NULL,
    name TEXT NOT NULL,
    original_input TEXT NOT NULL,
    workspace_path TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK (status IN ('ACTIVE', 'COMPLETED', 'ARCHIVED'))
) STRICT;

CREATE TABLE IF NOT EXISTS tasks (
    id TEXT PRIMARY KEY NOT NULL,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    agent_id TEXT NOT NULL DEFAULT 'default',
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'PENDING' CHECK (state IN ('PENDING', 'READY', 'QUEUED', 'RUNNING', 'BLOCKED', 'COMPLETED', 'FAILED', 'FAILED_REQUIRES_HUMAN', 'IN_CONSIDERATION')),
    assignee TEXT NOT NULL DEFAULT 'SYSTEM' CHECK (assignee IN ('SYSTEM', 'HUMAN')),
    os_process_id INTEGER,
    started_at TEXT,
    completed_at TEXT,
    last_heartbeat TEXT,
    retry_count INTEGER NOT NULL DEFAULT 0,
    token_usage INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK (retry_count >= 0),
    CHECK (token_usage >= 0)
) STRICT;

CREATE TABLE IF NOT EXISTS task_relations (
    parent_task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    child_task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    relation_type TEXT NOT NULL DEFAULT 'BLOCKS',
    PRIMARY KEY (parent_task_id, child_task_id),
    CHECK (parent_task_id != child_task_id),
    CHECK (relation_type IN ('BLOCKS', 'SPAWNED_BY', 'DEPENDS_ON'))
) STRICT;

CREATE TABLE IF NOT EXISTS events (
    id TEXT PRIMARY KEY NOT NULL,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    task_id TEXT REFERENCES tasks(id) ON DELETE CASCADE,
    type TEXT NOT NULL,
    payload TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT ''
) STRICT;

CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS agent_profiles (
    id TEXT PRIMARY KEY NOT NULL,
    name TEXT NOT NULL,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    temperature REAL NOT NULL DEFAULT 0.7,
    system_prompt TEXT,
    role TEXT NOT NULL DEFAULT 'CODE_GEN',
    max_tokens INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL,
    CHECK (max_tokens >= 0)
) STRICT;

CREATE TABLE IF NOT EXISTS memories (
    id TEXT PRIMARY KEY NOT NULL,
    scope TEXT NOT NULL DEFAULT 'GLOBAL',
    project_id TEXT REFERENCES projects(id) ON DELETE SET NULL,
    tags TEXT,
    symptom TEXT,
    solution TEXT,
    created_at TEXT NOT NULL,
    last_accessed_at TEXT,
    access_count INTEGER NOT NULL DEFAULT 0,
    superseded_by TEXT REFERENCES memories(id) ON DELETE SET NULL,
    CHECK (scope IN ('GLOBAL', 'PROJECT', 'TASK_CURATION', 'USER_PREFERENCE')),
    CHECK (access_count >= 0)
) STRICT;

CREATE INDEX IF NOT EXISTS idx_tasks_state_assignee ON tasks(state, assignee);
CREATE INDEX IF NOT EXISTS idx_tasks_project ON tasks(project_id);
CREATE INDEX IF NOT EXISTS idx_tasks_project_state ON tasks(project_id, state);
CREATE INDEX IF NOT EXISTS idx_tasks_heartbeat ON tasks(last_heartbeat);
CREATE INDEX IF NOT EXISTS idx_task_relations_child ON task_relations(child_task_id);
CREATE INDEX IF NOT EXISTS idx_task_relations_parent ON task_relations(parent_task_id);
CREATE INDEX IF NOT EXISTS idx_events_task ON events(task_id);
CREATE INDEX IF NOT EXISTS idx_events_task_created_at ON events(task_id, created_at);
CREATE INDEX IF NOT EXISTS idx_events_project ON events(project_id);
CREATE INDEX IF NOT EXISTS idx_events_project_created_at ON events(project_id, created_at);
CREATE INDEX IF NOT EXISTS idx_memories_project ON memories(project_id);
CREATE INDEX IF NOT EXISTS idx_memories_scope_project ON memories(scope, project_id);
CREATE INDEX IF NOT EXISTS idx_memories_superseded_by ON memories(superseded_by);
