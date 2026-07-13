import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, expect, test, vi } from "vitest";
import {
  UnauthorizedError,
  useCollection,
  useImportItems,
  useSetQuantity,
  useWantlist,
} from "./api";

function wrapper({ children }: { children: ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

test("useCollection returns items and totals, coalescing null arrays", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(jsonResponse({ items: null, total: 0, totalCopies: 0 })),
  );
  const { result } = renderHook(() => useCollection("", 0), { wrapper });
  await waitFor(() => expect(result.current.isSuccess).toBe(true));
  expect(result.current.data).toEqual({ items: [], total: 0, totalCopies: 0 });
});

test("useCollection throws UnauthorizedError on 401", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(jsonResponse({ title: "Unauthorized", status: 401 }, 401)),
  );
  const { result } = renderHook(() => useCollection("", 0), { wrapper });
  await waitFor(() => expect(result.current.isError).toBe(true));
  expect(result.current.error).toBeInstanceOf(UnauthorizedError);
});

test("useSetQuantity PUTs the quantity", async () => {
  const fetchMock = vi
    .fn()
    .mockResolvedValue(jsonResponse({ item: { scryfallId: "s1", quantity: 3 } }));
  vi.stubGlobal("fetch", fetchMock);
  const { result } = renderHook(() => useSetQuantity(), { wrapper });
  result.current.mutate({ scryfallId: "s1", quantity: 3 });
  await waitFor(() => expect(result.current.isSuccess).toBe(true));
  // openapi-fetch may call fetch(Request) or fetch(url, init) — handle both.
  const [input, init] = fetchMock.mock.calls[0] as [Request | string, RequestInit | undefined];
  const url = typeof input === "string" ? input : input.url;
  const method = init?.method ?? (typeof input === "string" ? undefined : input.method);
  const rawBody =
    init?.body ?? (typeof input === "string" ? undefined : await input.clone().text());
  expect(url).toContain("/api/collection/cards/s1");
  expect(method).toBe("PUT");
  expect(JSON.parse(rawBody as string)).toEqual({ quantity: 3 });
});

test("useImportItems surfaces the summary", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse({ addedRows: 2, updatedRows: 1 })));
  const { result } = renderHook(() => useImportItems(), { wrapper });
  result.current.mutate({ items: [{ scryfallId: "s1", quantity: 2 }] });
  await waitFor(() => expect(result.current.isSuccess).toBe(true));
  expect(result.current.data).toEqual({ addedRows: 2, updatedRows: 1 });
});

test("useWantlist throws UnauthorizedError on 401", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(jsonResponse({ title: "Unauthorized", status: 401 }, 401)),
  );
  const { result } = renderHook(() => useWantlist("cube-1"), { wrapper });
  await waitFor(() => expect(result.current.isError).toBe(true));
  expect(result.current.error).toBeInstanceOf(UnauthorizedError);
});
