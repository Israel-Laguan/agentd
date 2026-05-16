import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { SidebarItem } from '.';

const MockIcon = ({ size, className }: { size: number; className?: string }) => (
  <svg width={size} height={size} className={className} data-testid="mock-icon" />
);

describe('SidebarItem', () => {
  it('renders label', () => {
    render(<SidebarItem icon={MockIcon} label="Intake Console" />);
    expect(screen.getByText('Intake Console')).toBeInTheDocument();
  });

  it('calls onClick when clicked', async () => {
    const onClick = vi.fn();
    render(<SidebarItem icon={MockIcon} label="Board" onClick={onClick} />);
    await userEvent.click(screen.getByRole('button', { name: 'Board' }));
    expect(onClick).toHaveBeenCalledOnce();
  });

  it('applies active styles when active', () => {
    render(<SidebarItem icon={MockIcon} label="Active Tab" active />);
    const button = screen.getByRole('button', { name: 'Active Tab' });
    expect(button).toHaveClass('font-semibold');
  });
});
