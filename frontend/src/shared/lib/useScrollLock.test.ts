import { renderHook } from "@testing-library/react";
import { afterEach, expect, test } from "vitest";
import { useScrollLock } from "./useScrollLock";

afterEach(() => {
  document.body.style.overflow = "";
  document.body.style.paddingRight = "";
  window.scrollY = 0;
});

test("locks body scroll while active and restores on release", () => {
  const { unmount } = renderHook(() => useScrollLock(true));
  expect(document.body.style.overflow).toBe("hidden");
  unmount();
  expect(document.body.style.overflow).toBe("");
});

test("inactive callers never touch the body", () => {
  renderHook(() => useScrollLock(false));
  expect(document.body.style.overflow).toBe("");
});

test("ref-counts concurrent locks so the last release wins", () => {
  const outer = renderHook(() => useScrollLock(true));
  const inner = renderHook(() => useScrollLock(true));
  expect(document.body.style.overflow).toBe("hidden");
  inner.unmount();
  // Outer overlay is still open — the body must stay locked.
  expect(document.body.style.overflow).toBe("hidden");
  outer.unmount();
  expect(document.body.style.overflow).toBe("");
});

test("toggling active off releases without unmounting", () => {
  const { rerender } = renderHook(({ on }) => useScrollLock(on), {
    initialProps: { on: true },
  });
  expect(document.body.style.overflow).toBe("hidden");
  rerender({ on: false });
  expect(document.body.style.overflow).toBe("");
});

test("restores the scroll position the page had when locked", () => {
  window.scrollY = 250;
  const restore: number[] = [];
  const original = window.scrollTo;
  window.scrollTo = ((_x: number, y: number) => {
    restore.push(y);
  }) as unknown as typeof window.scrollTo;
  const { unmount } = renderHook(() => useScrollLock(true));
  unmount();
  window.scrollTo = original;
  expect(restore).toEqual([250]);
});
