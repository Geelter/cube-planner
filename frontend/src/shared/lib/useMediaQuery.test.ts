import { act, renderHook } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { useMediaQuery } from "./useMediaQuery";

afterEach(() => vi.unstubAllGlobals());

function stubMatchMedia(initialMatches: boolean) {
  let matches = initialMatches;
  const listeners = new Set<() => void>();
  vi.stubGlobal("matchMedia", (query: string) => ({
    get matches() {
      return matches;
    },
    media: query,
    addEventListener: (_: string, cb: () => void) => listeners.add(cb),
    removeEventListener: (_: string, cb: () => void) => listeners.delete(cb),
  }));
  return (next: boolean) => {
    matches = next;
    listeners.forEach((cb) => cb());
  };
}

test("reflects the current match and reacts to changes", () => {
  const setMatches = stubMatchMedia(false);
  const { result } = renderHook(() => useMediaQuery("(min-width: 768px)"));
  expect(result.current).toBe(false);
  act(() => setMatches(true));
  expect(result.current).toBe(true);
});
