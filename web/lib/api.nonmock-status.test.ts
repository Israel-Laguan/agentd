import { describe, it, expect, vi } from 'vitest';
import { useNonMockMode, NON_MOCK_ENV } from './api.nonmock.shared';

describe('API (non-mock status)', () => {
  const enableNonMock = useNonMockMode();

  it('getWorkforce returns null when no workforce endpoint exists', async () => {
    enableNonMock();
    const { getWorkforce } = await NON_MOCK_ENV.importApi();
    expect(await getWorkforce()).toBeNull();
  });

  it('getSystemStatus parses total_token_usage from daemon envelope', async () => {
    enableNonMock();
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

    const { getSystemStatus } = await NON_MOCK_ENV.importApi();
    const status = await getSystemStatus();
    expect(status.total_token_usage).toBe(99);
    expect(status.rolling_token_remaining).toBe(0);
    expect(status.status?.summary.tasks_by_state.RUNNING).toBe(3);
    expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining('/api/v1/system/status'));
  });
});
