import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { getBoard, getWorkforce, getSystemStatus, sendChat, updateTask, fetchTaskComments, addTaskComment, fetchProviders } from './api';
import { mockSystemStatus } from './mocks/system.mock';
import { mockBoard } from './mocks/board.mock';
import { mockWorkforce } from './mocks/workforce.mock';
import { mockTaskComments } from './mocks/mock-task-comment';
import { mockProviders } from './mocks/providers.mock';
import { TaskStatus } from './types';

const initialTasks = structuredClone(mockBoard.tasks);
const initialComments = structuredClone(mockTaskComments);

describe('API (mock mode)', () => {
  beforeEach(() => {
    mockBoard.tasks.splice(0, mockBoard.tasks.length, ...structuredClone(initialTasks));
    mockTaskComments.splice(0, mockTaskComments.length, ...structuredClone(initialComments));
  });

  it('getBoard returns mock board', async () => {
    const board = await getBoard();
    expect(board).toEqual(mockBoard);
  });

  it('getSystemStatus returns mock system status', async () => {
    const status = await getSystemStatus();
    expect(status).toEqual(mockSystemStatus);
    expect(status.total_token_usage).toBeGreaterThan(0);
  });

  it('getWorkforce returns mock workforce', async () => {
    const workforce = await getWorkforce();
    expect(workforce).toEqual(mockWorkforce);
  });

  it('sendChat returns assistant message', async () => {
    const response = await sendChat('hello');
    expect(response.message.role).toBe('assistant');
    expect(response.message.content).toContain('hello');
  });

  it('updateTask mutates and returns the task', async () => {
    const original = mockBoard.tasks[0];
    const updated = await updateTask(original.id, { state: TaskStatus.COMPLETED });
    expect(updated.state).toBe(TaskStatus.COMPLETED);
    expect(updated.id).toBe(original.id);
    expect(mockBoard.tasks[0].state).toBe(TaskStatus.COMPLETED);
  });

  it('fetchTaskComments filters by taskId', async () => {
    const comments = await fetchTaskComments('t1');
    expect(comments.length).toBeGreaterThan(0);
    expect(comments.every((c: { task_id: string }) => c.task_id === 't1')).toBe(true);
  });

  it('addTaskComment appends a comment', async () => {
    const before = mockTaskComments.length;
    const comment = await addTaskComment('t1', 'test comment');
    expect(comment.task_id).toBe('t1');
    expect(comment.message).toBe('test comment');
    expect(mockTaskComments.length).toBe(before + 1);
  });

  it('fetchProviders returns mock provider list', async () => {
    const providers = await fetchProviders();
    expect(providers).toEqual(mockProviders);
    expect(providers.length).toBeGreaterThan(0);
    expect(providers[0]).toMatchObject({ name: expect.any(String), adapter: expect.any(String), models: expect.any(Array) });
  });
});

describe('API (non-mock mode)', () => {
  const originalUseMock = process.env.NEXT_PUBLIC_USE_MOCK;

  afterEach(() => {
    if (originalUseMock === undefined) {
      delete process.env.NEXT_PUBLIC_USE_MOCK;
    } else {
      process.env.NEXT_PUBLIC_USE_MOCK = originalUseMock;
    }
    vi.resetModules();
    vi.unstubAllGlobals();
  });

  it('getWorkforce returns null when no workforce endpoint exists', async () => {
    process.env.NEXT_PUBLIC_USE_MOCK = 'false';
    vi.resetModules();
    const { getWorkforce } = await import('./api');
    expect(await getWorkforce()).toBeNull();
  });

  it('getSystemStatus parses total_token_usage from daemon envelope', async () => {
    process.env.NEXT_PUBLIC_USE_MOCK = 'false';
    vi.resetModules();
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        data: {
          total_token_usage: 99,
          rolling_token_remaining: 0,
          status: { summary: { tasks_by_state: { RUNNING: 3 } } },
        },
      }),
    });
    vi.stubGlobal('fetch', fetchMock);

    const { getSystemStatus } = await import('./api');
    const status = await getSystemStatus();
    expect(status.total_token_usage).toBe(99);
    expect(status.rolling_token_remaining).toBe(0);
    expect(status.status?.summary.tasks_by_state.RUNNING).toBe(3);
    expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining('/api/v1/system/status'));
  });

  it('sendChat returns plan from tool_calls (create_plan)', async () => {
    process.env.NEXT_PUBLIC_USE_MOCK = 'false';
    vi.resetModules();
    const planArgs = JSON.stringify({
      project_name: 'Todo API',
      description: 'A REST API',
      tasks: [{ title: 'Setup DB', description: 'init schema', ref_id: 'task-1' }],
    });
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        id: 'cmpl-1',
        choices: [{
          message: {
            role: 'assistant',
            content: null,
            tool_calls: [{ function: { name: 'create_plan', arguments: planArgs } }],
          },
          finish_reason: 'tool_calls',
        }],
      }),
    }));
    const { sendChat } = await import('./api');
    const res = await sendChat('Build a todo API');
    expect(res.plan).toBeDefined();
    expect(res.plan!.name).toBe('Todo API');
    expect(res.plan!.tasks).toHaveLength(1);
    expect(res.plan!.tasks[0]).toEqual({ id: 'task-1', title: 'Setup DB', description: 'init schema' });
    expect(res.message.content).toContain('draft plan');
  });

  it('sendChat does not set plan for scope_clarification JSON in content', async () => {
    process.env.NEXT_PUBLIC_USE_MOCK = 'false';
    vi.resetModules();
    const clarification = JSON.stringify({
      kind: 'scope_clarification',
      scopes: [{ id: 'backend', description: 'Backend API' }],
    });
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        id: 'cmpl-5',
        choices: [{ message: { role: 'assistant', content: clarification }, finish_reason: 'stop' }],
      }),
    }));
    const { sendChat } = await import('./api');
    const res = await sendChat('build something big');
    expect(res.plan).toBeUndefined();
    expect(res.message.content).toBe(clarification);
  });

  it('sendChat returns plan from message.content fallback (no tool_calls)', async () => {
    process.env.NEXT_PUBLIC_USE_MOCK = 'false';
    vi.resetModules();
    const planContent = JSON.stringify({
      project_name: 'Fallback Project',
      description: 'Content only',
      tasks: [{ title: 'Task One', description: 'do it' }],
    });
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        id: 'cmpl-2',
        choices: [{ message: { role: 'assistant', content: planContent }, finish_reason: 'stop' }],
      }),
    }));
    const { sendChat } = await import('./api');
    const res = await sendChat('plan something');
    expect(res.plan).toBeDefined();
    expect(res.plan!.name).toBe('Fallback Project');
    expect(res.plan!.tasks[0].title).toBe('Task One');
  });

  it('sendChat returns no plan for plain text content', async () => {
    process.env.NEXT_PUBLIC_USE_MOCK = 'false';
    vi.resetModules();
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        id: 'cmpl-3',
        choices: [{ message: { role: 'assistant', content: 'Hello, how can I help?' }, finish_reason: 'stop' }],
      }),
    }));
    const { sendChat } = await import('./api');
    const res = await sendChat('hello');
    expect(res.plan).toBeUndefined();
    expect(res.message.content).toBe('Hello, how can I help?');
  });

  it('sendChat forwards approved_scopes in request body', async () => {
    process.env.NEXT_PUBLIC_USE_MOCK = 'false';
    vi.resetModules();
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        id: 'cmpl-4',
        choices: [{ message: { role: 'assistant', content: 'Generating plan...' }, finish_reason: 'stop' }],
      }),
    });
    vi.stubGlobal('fetch', fetchMock);
    const { sendChat } = await import('./api');
    await sendChat('build it', undefined, ['backend-api']);
    const body = JSON.parse(fetchMock.mock.calls[0][1].body);
    expect(body.approved_scopes).toEqual(['backend-api']);
    expect(body.tools).toBeDefined();
    expect(body.tools[0].function.name).toBe('create_plan');
  });

  it('sendChat returns statusReport from status_report tool_calls', async () => {
    process.env.NEXT_PUBLIC_USE_MOCK = 'false';
    vi.resetModules();
    const statusArgs = JSON.stringify({
      kind: 'status_report',
      message: 'You have 1 active project(s) with 2 task(s) remaining',
      summary: { total_projects: 1, tasks_by_state: { RUNNING: 1, READY: 1 } },
    });
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        id: 'cmpl-status',
        choices: [{
          message: {
            role: 'assistant',
            content: statusArgs,
            tool_calls: [{ function: { name: 'status_report', arguments: statusArgs } }],
          },
          finish_reason: 'tool_calls',
        }],
      }),
    }));
    const { sendChat } = await import('./api');
    const res = await sendChat('status?');
    expect(res.statusReport).toBeDefined();
    expect(res.statusReport!.totalProjects).toBe(1);
    expect(res.message.content).toBe('You have 1 active project(s) with 2 task(s) remaining');
    expect(res.message.content).not.toContain('"kind"');
    expect(res.plan).toBeUndefined();
  });

  it('sendChat returns intentClarification without raw JSON bubble text', async () => {
    process.env.NEXT_PUBLIC_USE_MOCK = 'false';
    vi.resetModules();
    const clarification = JSON.stringify({
      kind: 'intent_clarification',
      message: "I'm not sure what you need.",
    });
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        id: 'cmpl-intent',
        choices: [{ message: { role: 'assistant', content: clarification }, finish_reason: 'stop' }],
      }),
    }));
    const { sendChat } = await import('./api');
    const res = await sendChat('Hello');
    expect(res.intentClarification?.message).toContain('not sure');
    expect(res.message.content).toBe("I'm not sure what you need.");
    expect(res.plan).toBeUndefined();
  });

  it('sendChat returns scopeClarification with label mapped scopes', async () => {
    process.env.NEXT_PUBLIC_USE_MOCK = 'false';
    vi.resetModules();
    const clarification = JSON.stringify({
      kind: 'scope_clarification',
      message: 'Multiple projects detected.',
      scopes: [
        { id: 'backend-api', label: 'Backend API service' },
        { id: 'frontend-ui', label: 'Frontend UI' },
      ],
    });
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        id: 'cmpl-scope',
        choices: [{ message: { role: 'assistant', content: clarification }, finish_reason: 'stop' }],
      }),
    }));
    const { sendChat } = await import('./api');
    const res = await sendChat('Build API and UI');
    expect(res.scopeClarification?.scopes).toHaveLength(2);
    expect(res.scopeClarification!.scopes[0].label).toBe('Backend API service');
    expect(res.message.content).toBe('Multiple projects detected.');
  });

  it('postApprovePlan POSTs to materialize with project_name body', async () => {
    process.env.NEXT_PUBLIC_USE_MOCK = 'false';
    vi.resetModules();
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        data: { project: { id: 'p1' }, tasks: [{ id: 'task-a', title: 'Do thing' }] },
      }),
    });
    vi.stubGlobal('fetch', fetchMock);
    const { postApprovePlan } = await import('./api');
    const result = await postApprovePlan({ name: 'My Project', description: 'desc', tasks: [{ id: 't1', title: 'Do thing', description: 'do it' }] });
    expect(result.projectId).toBe('p1');
    expect(result.taskIds).toEqual(['task-a']);
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/projects/materialize'),
      expect.objectContaining({ method: 'POST' })
    );
    const body = JSON.parse(fetchMock.mock.calls[0][1].body);
    expect(body.project_name).toBe('My Project');
    expect(body.tasks[0].temp_id).toBe('t1');
    expect(body.tasks[0].title).toBe('Do thing');
  });

  it('postApprovePlan sends materialize token header when env set', async () => {
    process.env.NEXT_PUBLIC_USE_MOCK = 'false';
    process.env.NEXT_PUBLIC_MATERIALIZE_TOKEN = 'secret-token';
    vi.resetModules();
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ data: {} }),
    });
    vi.stubGlobal('fetch', fetchMock);
    const { postApprovePlan } = await import('./api');
    await postApprovePlan({ name: 'P', description: '', tasks: [] });
    const headers = fetchMock.mock.calls[0][1].headers;
    expect(headers['X-Agentd-Materialize-Token']).toBe('secret-token');
    delete process.env.NEXT_PUBLIC_MATERIALIZE_TOKEN;
  });

  it('postApprovePlan omits token header when env unset', async () => {
    process.env.NEXT_PUBLIC_USE_MOCK = 'false';
    delete process.env.NEXT_PUBLIC_MATERIALIZE_TOKEN;
    vi.resetModules();
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ data: {} }),
    });
    vi.stubGlobal('fetch', fetchMock);
    const { postApprovePlan } = await import('./api');
    await postApprovePlan({ name: 'P', description: '', tasks: [] });
    const headers = fetchMock.mock.calls[0][1].headers;
    expect(headers['X-Agentd-Materialize-Token']).toBeUndefined();
  });
});
