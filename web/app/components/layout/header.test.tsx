import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Header } from './header';

describe('Header', () => {
  it('renders daemon status and ref', () => {
    render(<Header onStartNewIntake={() => {}} />);
    expect(screen.getByText('Daemon Online')).toBeInTheDocument();
    expect(screen.getByText('REF: EXPR-API-V2')).toBeInTheDocument();
  });

  it('calls onStartNewIntake when button is clicked', async () => {
    const onStartNewIntake = vi.fn();
    render(<Header onStartNewIntake={onStartNewIntake} />);
    const button = screen.getByRole('button', { name: /new intake/i });
    await userEvent.click(button);
    expect(onStartNewIntake).toHaveBeenCalledOnce();
  });
});
