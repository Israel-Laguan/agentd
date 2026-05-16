import { describe, it, expect, beforeEach } from 'vitest';
import { getBoard, getWorkforce, sendChat, updateTask, fetchTaskComments, addTaskComment } from './api';
import { mockBoard } from './mocks/board.mock';
import { mockWorkforce } from './mocks/workforce.mock';
import { mockTaskComments } from './mocks/mock-task-comment';
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
    const updated = await updateTask(original.id, { status: TaskStatus.COMPLETED });
    expect(updated.status).toBe(TaskStatus.COMPLETED);
    expect(updated.id).toBe(original.id);
    expect(mockBoard.tasks[0].status).toBe(TaskStatus.COMPLETED);
  });

  it('fetchTaskComments filters by taskId', async () => {
    const comments = await fetchTaskComments('t1');
    expect(comments.length).toBeGreaterThan(0);
    expect(comments.every((c: { taskId: string }) => c.taskId === 't1')).toBe(true);
  });

  it('addTaskComment appends a comment', async () => {
    const before = mockTaskComments.length;
    const comment = await addTaskComment('t1', 'test comment');
    expect(comment.taskId).toBe('t1');
    expect(comment.message).toBe('test comment');
    expect(mockTaskComments.length).toBe(before + 1);
  });
});
