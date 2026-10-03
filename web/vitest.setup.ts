import '@testing-library/jest-dom/vitest';
import { cleanup } from '@testing-library/react';
import { afterEach } from 'vitest';

// jsdom does not define Element.prototype.scrollIntoView, so mounting
// TaskDrawer throws in CommentPanel's auto-scroll effect. Stub it with a no-op.
if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = function scrollIntoView() {};
}

afterEach(() => {
  cleanup();
});
