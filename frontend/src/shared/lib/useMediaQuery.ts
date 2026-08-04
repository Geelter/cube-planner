import { useCallback, useSyncExternalStore } from "react";

// Reactive matchMedia. NOTE: jsdom does not implement matchMedia — tests
// rendering consumers under `// @vitest-environment jsdom` must stub it
// (happy-dom provides a non-matching default).
export function useMediaQuery(query: string): boolean {
  const subscribe = useCallback(
    (onChange: () => void) => {
      const mql = matchMedia(query);
      mql.addEventListener("change", onChange);
      return () => mql.removeEventListener("change", onChange);
    },
    [query],
  );
  return useSyncExternalStore(subscribe, () => matchMedia(query).matches);
}
