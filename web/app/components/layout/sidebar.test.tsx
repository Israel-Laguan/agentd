import { afterEach, describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Sidebar, SIDEBAR_COLLAPSED_KEY } from './sidebar';

const workforce = { active_workers: 2, max_workers: 4, queue_length: 0 };

const renderSidebar = () =>
  render(<Sidebar activeTab="board" setActiveTab={vi.fn()} workforce={workforce} />);

describe('Sidebar', () => {
  afterEach(() => {
    window.localStorage.clear();
    vi.restoreAllMocks();
  });

  it('starts expanded, with labels and the Load Status panel', () => {
    const { container } = renderSidebar();
    expect(container.querySelector('aside')).toHaveClass('w-60');
    expect(screen.getByText('Workforce Board')).toBeInTheDocument();
    expect(screen.getByText('Load Status')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Collapse sidebar' })).toHaveAttribute('aria-expanded', 'true');
  });

  it('collapses to an icon rail whose items keep their names', async () => {
    const { container } = renderSidebar();
    await userEvent.click(screen.getByRole('button', { name: 'Collapse sidebar' }));

    expect(container.querySelector('aside')).toHaveClass('w-16');
    expect(screen.queryByText('Workforce Board')).not.toBeInTheDocument();
    const item = screen.getByRole('button', { name: 'Workforce Board' });
    expect(item).toHaveAttribute('title', 'Workforce Board');
    expect(screen.getByRole('button', { name: 'Expand sidebar' })).toHaveAttribute('aria-expanded', 'false');
  });

  it('shows the load as a compact badge when collapsed', async () => {
    renderSidebar();
    await userEvent.click(screen.getByRole('button', { name: 'Collapse sidebar' }));

    expect(screen.queryByText('Load Status')).not.toBeInTheDocument();
    expect(screen.getByRole('status', { name: 'Load Status 2/4' })).toHaveTextContent('2/4');
  });

  it('remembers the choice in localStorage', async () => {
    renderSidebar();
    await userEvent.click(screen.getByRole('button', { name: 'Collapse sidebar' }));
    expect(window.localStorage.getItem(SIDEBAR_COLLAPSED_KEY)).toBe('true');

    await userEvent.click(screen.getByRole('button', { name: 'Expand sidebar' }));
    expect(window.localStorage.getItem(SIDEBAR_COLLAPSED_KEY)).toBe('false');
  });

  it('opens collapsed when the stored choice says so', async () => {
    window.localStorage.setItem(SIDEBAR_COLLAPSED_KEY, 'true');
    const { container } = renderSidebar();

    expect(await screen.findByRole('button', { name: 'Expand sidebar' })).toBeInTheDocument();
    expect(container.querySelector('aside')).toHaveClass('w-16');
  });

  it('still toggles when storage is blocked', async () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('blocked'); });
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('blocked'); });
    const { container } = renderSidebar();

    await userEvent.click(screen.getByRole('button', { name: 'Collapse sidebar' }));
    expect(container.querySelector('aside')).toHaveClass('w-16');
  });
});
