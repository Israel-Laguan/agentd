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
});
