import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { useNonMockMode, NON_MOCK_ENV } from './api.nonmock.shared';

describe('API (non-mock materialize)', () => {
  const enableNonMock = useNonMockMode();
  let originalToken: string | undefined;

  beforeEach(() => {
    originalToken = process.env.NEXT_PUBLIC_MATERIALIZE_TOKEN;
  });

  afterEach(() => {
    if (originalToken === undefined) {
      delete process.env.NEXT_PUBLIC_MATERIALIZE_TOKEN;
    } else {
      process.env.NEXT_PUBLIC_MATERIALIZE_TOKEN = originalToken;
    }
  });

  it('postApprovePlan POSTs to materialize with project_name body', async () => {
    enableNonMock();
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        data: { project: { id: 'p1' }, tasks: [{ id: 'task-a', title: 'Do thing' }] },
      }),
    });
    vi.stubGlobal('fetch', fetchMock);
    const { postApprovePlan } = await NON_MOCK_ENV.importApi();
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

  it('postApprovePlan accepts flat project id in response', async () => {
    enableNonMock();
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ data: { id: 'flat-p1', tasks: [{ id: 'task-b' }] } }),
    });
    vi.stubGlobal('fetch', fetchMock);
    const { postApprovePlan } = await NON_MOCK_ENV.importApi();
    const result = await postApprovePlan({ name: 'Flat', description: '', tasks: [] });
    expect(result.projectId).toBe('flat-p1');
    expect(result.taskIds).toEqual(['task-b']);
  });

  it('postApprovePlan sends materialize token header when env set', async () => {
    enableNonMock();
    process.env.NEXT_PUBLIC_MATERIALIZE_TOKEN = 'secret-token';
    vi.resetModules();
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ data: { id: 'p1' } }),
    });
    vi.stubGlobal('fetch', fetchMock);
    const { postApprovePlan } = await NON_MOCK_ENV.importApi();
    await postApprovePlan({ name: 'P', description: '', tasks: [] });
    const headers = fetchMock.mock.calls[0][1].headers;
    expect(headers['X-Agentd-Materialize-Token']).toBe('secret-token');
  });

  it('postApprovePlan omits token header when env unset', async () => {
    enableNonMock();
    delete process.env.NEXT_PUBLIC_MATERIALIZE_TOKEN;
    vi.resetModules();
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ data: { id: 'p1' } }),
    });
    vi.stubGlobal('fetch', fetchMock);
    const { postApprovePlan } = await NON_MOCK_ENV.importApi();
    await postApprovePlan({ name: 'P', description: '', tasks: [] });
    const headers = fetchMock.mock.calls[0][1].headers;
    expect(headers['X-Agentd-Materialize-Token']).toBeUndefined();
  });
});
