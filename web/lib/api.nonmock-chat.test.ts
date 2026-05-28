import { describe, it, expect, vi } from 'vitest';
import { useNonMockMode, NON_MOCK_ENV } from './api.nonmock.shared';

describe('API (non-mock chat)', () => {
  const enableNonMock = useNonMockMode();

  it('sendChat returns plan from tool_calls (create_plan)', async () => {
    enableNonMock();
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
    const { sendChat } = await NON_MOCK_ENV.importApi();
    const res = await sendChat('Build a todo API');
    expect(res.plan).toBeDefined();
    expect(res.plan!.name).toBe('Todo API');
    expect(res.plan!.tasks).toHaveLength(1);
    expect(res.plan!.tasks[0]).toEqual({ id: 'task-1', title: 'Setup DB', description: 'init schema' });
    expect(res.message.content).toContain('draft plan');
  });

  it('sendChat does not set plan for scope_clarification JSON in content', async () => {
    enableNonMock();
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
    const { sendChat } = await NON_MOCK_ENV.importApi();
    const res = await sendChat('build something big');
    expect(res.plan).toBeUndefined();
    expect(res.message.content).toBe(clarification);
  });

  it('sendChat returns plan from message.content fallback (no tool_calls)', async () => {
    enableNonMock();
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
    const { sendChat } = await NON_MOCK_ENV.importApi();
    const res = await sendChat('plan something');
    expect(res.plan).toBeDefined();
    expect(res.plan!.name).toBe('Fallback Project');
    expect(res.plan!.tasks[0].title).toBe('Task One');
  });

  it('sendChat returns no plan for plain text content', async () => {
    enableNonMock();
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        id: 'cmpl-3',
        choices: [{ message: { role: 'assistant', content: 'Hello, how can I help?' }, finish_reason: 'stop' }],
      }),
    }));
    const { sendChat } = await NON_MOCK_ENV.importApi();
    const res = await sendChat('hello');
    expect(res.plan).toBeUndefined();
    expect(res.message.content).toBe('Hello, how can I help?');
  });

  it('sendChat forwards approved_scopes in request body', async () => {
    enableNonMock();
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        id: 'cmpl-4',
        choices: [{ message: { role: 'assistant', content: 'Generating plan...' }, finish_reason: 'stop' }],
      }),
    });
    vi.stubGlobal('fetch', fetchMock);
    const { sendChat } = await NON_MOCK_ENV.importApi();
    await sendChat('build it', undefined, ['backend-api']);
    const body = JSON.parse(fetchMock.mock.calls[0][1].body);
    expect(body.approved_scopes).toEqual(['backend-api']);
    expect(body.tools).toBeDefined();
    expect(body.tools[0].function.name).toBe('create_plan');
  });

  it('sendChat returns statusReport from status_report tool_calls', async () => {
    enableNonMock();
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
    const { sendChat } = await NON_MOCK_ENV.importApi();
    const res = await sendChat('status?');
    expect(res.statusReport).toBeDefined();
    expect(res.statusReport!.totalProjects).toBe(1);
    expect(res.message.content).toBe('You have 1 active project(s) with 2 task(s) remaining');
    expect(res.message.content).not.toContain('"kind"');
    expect(res.plan).toBeUndefined();
  });

  it('sendChat returns intentClarification without raw JSON bubble text', async () => {
    enableNonMock();
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
    const { sendChat } = await NON_MOCK_ENV.importApi();
    const res = await sendChat('Hello');
    expect(res.intentClarification?.message).toContain('not sure');
    expect(res.message.content).toBe("I'm not sure what you need.");
    expect(res.plan).toBeUndefined();
  });

  it('sendChat returns scopeClarification with label mapped scopes', async () => {
    enableNonMock();
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
    const { sendChat } = await NON_MOCK_ENV.importApi();
    const res = await sendChat('Build API and UI');
    expect(res.scopeClarification?.scopes).toHaveLength(2);
    expect(res.scopeClarification!.scopes[0].label).toBe('Backend API service');
    expect(res.message.content).toBe('Multiple projects detected.');
  });
});
