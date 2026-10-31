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

  it('shows the icon alone when collapsed, named by aria-label and a tooltip', () => {
    render(<SidebarItem icon={MockIcon} label="Workforce Board" collapsed />);
    expect(screen.queryByText('Workforce Board')).not.toBeInTheDocument();
    const button = screen.getByRole('button', { name: 'Workforce Board' });
    expect(button).toHaveAttribute('title', 'Workforce Board');
    expect(screen.getByTestId('mock-icon')).toBeInTheDocument();
  });

  it('has no tooltip when expanded', () => {
    render(<SidebarItem icon={MockIcon} label="Board" />);
    expect(screen.getByRole('button', { name: 'Board' })).not.toHaveAttribute('title');
  });

  it('applies active styles when active', () => {
    render(<SidebarItem icon={MockIcon} label="Active Tab" active />);
    const button = screen.getByRole('button', { name: 'Active Tab' });
    expect(button).toHaveClass('font-semibold');
  });
});
