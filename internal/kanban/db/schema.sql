PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS projects (
    id TEXT PRIMARY KEY NOT NULL,
    name TEXT NOT NULL,
    original_input TEXT NOT NULL,
    workspace_path TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'ACTIVE',
    user_id TEXT NOT NULL DEFAULT '',
    started_empty INTEGER NOT NULL DEFAULT 0 CHECK (started_empty IN (0, 1)),
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
    state TEXT NOT NULL DEFAULT 'PENDING' CHECK (state IN ('PENDING', 'READY', 'QUEUED', 'RUNNING', 'BLOCKED', 'COMPLETED', 'FAILED', 'FAILED_REQUIRES_HUMAN', 'NEEDS_CONTEXT', 'IN_CONSIDERATION')),
    assignee TEXT NOT NULL DEFAULT 'SYSTEM' CHECK (assignee IN ('SYSTEM', 'HUMAN')),
    os_process_id INTEGER,
    started_at TEXT,
    completed_at TEXT,
    last_heartbeat TEXT,
    retry_count INTEGER NOT NULL DEFAULT 0,
    token_usage INTEGER NOT NULL DEFAULT 0,
    cached_token_usage INTEGER NOT NULL DEFAULT 0,
    cache_write_token_usage INTEGER NOT NULL DEFAULT 0,
    success_criteria TEXT NOT NULL DEFAULT '[]',
    criteria_met TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK (retry_count >= 0),
    CHECK (token_usage >= 0),
    CHECK (cached_token_usage >= 0),
    CHECK (cache_write_token_usage >= 0),
    CHECK (json_valid(success_criteria)),
    CHECK (json_type(success_criteria) = 'array'),
    CHECK (json_valid(criteria_met)),
    CHECK (json_type(criteria_met) = 'array')
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
    agentic_mode INTEGER NOT NULL DEFAULT 0,
    disable_topic_drift INTEGER NOT NULL DEFAULT 0,
    capability_route_intent TEXT,
    updated_at TEXT NOT NULL,
    CHECK (max_tokens >= 0),
    CHECK (agentic_mode IN (0, 1)),
    CHECK (disable_topic_drift IN (0, 1))
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

CREATE TABLE IF NOT EXISTS ai_calls (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    at TEXT NOT NULL,
    kind TEXT NOT NULL,
    purpose TEXT NOT NULL,
    stage TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT '',
    task_id TEXT NOT NULL DEFAULT '',
    agent_id TEXT NOT NULL DEFAULT '',
    correlation_id TEXT NOT NULL DEFAULT '',
    provider TEXT NOT NULL,
    model TEXT NOT NULL DEFAULT '',
    attempt TEXT NOT NULL,
    cascade INTEGER NOT NULL DEFAULT 0 CHECK (cascade IN (0, 1)),
    outcome TEXT NOT NULL,
    truncated INTEGER NOT NULL DEFAULT 0 CHECK (truncated IN (0, 1)),
    message_count INTEGER NOT NULL DEFAULT 0 CHECK (message_count >= 0),
    tool_count INTEGER NOT NULL DEFAULT 0 CHECK (tool_count >= 0),
    input_chars INTEGER NOT NULL DEFAULT 0 CHECK (input_chars >= 0),
    output_chars INTEGER NOT NULL DEFAULT 0 CHECK (output_chars >= 0),
    tokens INTEGER NOT NULL DEFAULT 0 CHECK (tokens >= 0),
    cached_tokens INTEGER NOT NULL DEFAULT 0 CHECK (cached_tokens >= 0),
    cache_write_tokens INTEGER NOT NULL DEFAULT 0 CHECK (cache_write_tokens >= 0),
    latency_ms INTEGER NOT NULL DEFAULT 0 CHECK (latency_ms >= 0)
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
CREATE INDEX IF NOT EXISTS idx_ai_calls_at_purpose ON ai_calls(at, purpose, stage, model);
CREATE INDEX IF NOT EXISTS idx_ai_calls_task ON ai_calls(task_id) WHERE task_id != '';
CREATE INDEX IF NOT EXISTS idx_ai_calls_correlation ON ai_calls(correlation_id) WHERE correlation_id != '';
