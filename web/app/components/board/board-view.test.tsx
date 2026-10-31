import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { BoardView } from './board-view';

// jsdom does no layout, so these tests pin the grid tracks themselves: a track
// that can shrink below a column's 240px lets the columns overlap (issue #159).
describe('BoardView', () => {
  const renderBoard = () => render(<BoardView tasks={[]} onDragEnd={vi.fn()} onTaskClick={vi.fn()} />);

  it('renders the ten status columns', () => {
    renderBoard();
    expect(screen.getByTestId('board-columns').children).toHaveLength(10);
  });

  it('never lets a grid track shrink below a column', () => {
    renderBoard();
    const grid = screen.getByTestId('board-columns');
    expect(grid).toHaveClass('grid-cols-[minmax(240px,1fr)]');
    expect(grid).toHaveClass('sm:grid-cols-[repeat(2,minmax(240px,1fr))]');
    expect(grid).toHaveClass('lg:grid-cols-[repeat(10,minmax(240px,1fr))]');
    expect(grid.className).not.toMatch(/(^|\s)(sm:|lg:)?grid-cols-\d+(\s|$)/);
  });

  it('scrolls sideways inside the board instead of the page', () => {
    renderBoard();
    expect(screen.getByTestId('board-columns')).toHaveClass('overflow-x-auto');
  });
});
