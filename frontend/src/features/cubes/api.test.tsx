import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, expect, test, vi } from "vitest";
import { CommitConflictError, useCommitChange, useCubeCards, useCubeList } from "./api";

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

test("useCubeList returns cubes and total", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(jsonResponse({ cubes: [{ id: "1", name: "Vintage" }], total: 1 })),
  );
  const { result } = renderHook(() => useCubeList("", 0), { wrapper });
  await waitFor(() => expect(result.current.isSuccess).toBe(true));
  expect(result.current.data?.total).toBe(1);
  expect(result.current.data?.cubes[0]?.name).toBe("Vintage");
});

test("useCubeCards coalesces null cards array", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse({ cards: null, version: 0 })));
  const { result } = renderHook(() => useCubeCards("cube-1"), { wrapper });
  await waitFor(() => expect(result.current.isSuccess).toBe(true));
  expect(result.current.data).toEqual({ cards: [], version: 0 });
});

test("useCommitChange throws CommitConflictError on 409", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        jsonResponse({ title: "Conflict", status: 409, type: "cube-version-conflict" }, 409),
      ),
  );
  const { result } = renderHook(() => useCommitChange("cube-1"), { wrapper });
  result.current.mutate({ expectedVersion: 0, adds: [{ scryfallId: "x", quantity: 1 }] });
  await waitFor(() => expect(result.current.isError).toBe(true));
  expect(result.current.error).toBeInstanceOf(CommitConflictError);
});

test("useCommitChange surfaces 422 problem detail", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        jsonResponse(
          { title: "Unprocessable Entity", status: 422, detail: "invalid cube change: empty diff" },
          422,
        ),
      ),
  );
  const { result } = renderHook(() => useCommitChange("cube-1"), { wrapper });
  result.current.mutate({ expectedVersion: 0 });
  await waitFor(() => expect(result.current.isError).toBe(true));
  expect(result.current.error?.message).toContain("empty diff");
});
