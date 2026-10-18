import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Header } from './header';

const noop = () => {};

describe('Header', () => {
  it.each([
    ['connecting', 'Connecting to daemon'],
    ['online', 'Daemon online'],
    ['unreachable', 'Daemon unreachable'],
  ] as const)('shows the daemon state %s', (daemon, text) => {
    render(<Header daemon={daemon} mock={false} status={null} onStartNewIntake={noop} />);
    expect(screen.getByText(text)).toBeInTheDocument();
    expect(screen.queryByText(/REF: EXPR-API-V2/)).not.toBeInTheDocument();
    expect(screen.queryByText('~/.agentd/projects/')).not.toBeInTheDocument();
  });

  it('labels mock mode instead of claiming a daemon', () => {
    render(<Header daemon="online" mock status={null} onStartNewIntake={noop} />);
    expect(screen.getByRole('status')).toHaveTextContent(/mock mode/i);
    expect(screen.queryByText('Daemon online')).not.toBeInTheDocument();
  });

  it('shows an open breaker from the daemon status', () => {
    const status = { total_token_usage: 0, breaker: { state: 'OPEN', failure_count: 3, last_error: 'provider 503' } };
    render(<Header daemon="online" mock={false} status={status} onStartNewIntake={noop} />);
    expect(screen.getByText(/breaker open/i)).toBeInTheDocument();
  });

  it('calls onStartNewIntake when button is clicked', async () => {
    const onStartNewIntake = vi.fn();
    render(<Header daemon="online" mock={false} status={null} onStartNewIntake={onStartNewIntake} />);
    const button = screen.getByRole('button', { name: /new intake/i });
    await userEvent.click(button);
    expect(onStartNewIntake).toHaveBeenCalledOnce();
  });
});
