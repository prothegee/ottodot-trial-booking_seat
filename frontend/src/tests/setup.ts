/**
 * Test setup, loaded once by vitest.
 *
 * It only adds the document matchers. There is no transport, no server, and no
 * browser to start, because every tier in this stack runs against a fake
 * transport by construction.
 */
import "@testing-library/jest-dom/vitest";
