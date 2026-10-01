import '@testing-library/jest-dom/vitest';
import { cleanup } from '@testing-library/react';
import { afterEach } from 'vitest';

// jsdom implements neither of these, and components call both on mount:
// CommentPanel scrolls its newest comment into view on every render, and
// framer-motion's AnimatePresence (used by TaskDrawer) queries scroll
// behaviour. Without the stubs, mounting any component that contains them
// throws rather than failing an assertion.
if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = function scrollIntoView() {};
}

afterEach(() => {
  cleanup();
});
