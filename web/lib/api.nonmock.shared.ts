import { afterEach, vi } from 'vitest';

export const NON_MOCK_ENV = {
  reset() {
    vi.resetModules();
    vi.unstubAllGlobals();
  },
  async importApi() {
    return import('./api');
  },
};

export function useNonMockMode() {
  const originalUseMock = process.env.NEXT_PUBLIC_USE_MOCK;

  afterEach(() => {
    if (originalUseMock === undefined) {
      delete process.env.NEXT_PUBLIC_USE_MOCK;
    } else {
      process.env.NEXT_PUBLIC_USE_MOCK = originalUseMock;
    }
    NON_MOCK_ENV.reset();
  });

  return () => {
    process.env.NEXT_PUBLIC_USE_MOCK = 'false';
    vi.resetModules();
  };
}
