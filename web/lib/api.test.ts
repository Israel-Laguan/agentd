import { describe, it, expect } from 'vitest';
import { getBoard, getWorkforce, sendChat, updateTask, fetchTaskComments, addTaskComment } from './api';
import { mockBoard } from './mocks/board.mock';
import { mockWorkforce } from './mocks/workforce.mock';
import { mockTaskComments } from './mocks/mock-task-comment';
import { TaskStatus } from './types';

describe('API (mock mode)', () => {
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
  });

  it('fetchTaskComments filters by taskId', async () => {
    const comments = await fetchTaskComments('t1');
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
