import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, expect, test, vi } from "vitest";
import { useCardAutocomplete } from "./api";

function wrapper({ children }: { children: ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

const bolt = {
  scryfallId: "ce711943-c1a1-43a0-8b89-8d169cfb8e06",
  oracleId: "4457ed35-7c10-48c8-9776-456485fdf070",
  name: "Lightning Bolt",
  manaCost: "{R}",
  typeLine: "Instant",
  imageSmall: null,
};

test("useCardAutocomplete stays idle under 2 chars", () => {
  const fetchSpy = vi.fn();
  vi.stubGlobal("fetch", fetchSpy);
  const { result } = renderHook(() => useCardAutocomplete("a"), { wrapper });
  expect(result.current.fetchStatus).toBe("idle");
  expect(fetchSpy).not.toHaveBeenCalled();
});

test("useCardAutocomplete returns cards", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ cards: [bolt] }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    ),
  );
  const { result } = renderHook(() => useCardAutocomplete("bolt"), { wrapper });
  await waitFor(() => expect(result.current.isSuccess).toBe(true));
  expect(result.current.data).toEqual([bolt]);
});
