import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, expect, test, vi } from "vitest";
import { useLogout, useMe, useRegister, useResetPassword } from "./api";

const mockNavigate = vi.fn().mockResolvedValue(undefined);
vi.mock("@tanstack/react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-router")>()),
  useNavigate: () => mockNavigate,
}));

afterEach(() => {
  mockNavigate.mockClear();
});

function wrapper({ children }: { children: ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

test("useMe returns null on 401", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ title: "Unauthorized", status: 401 }), {
        status: 401,
        headers: { "Content-Type": "application/problem+json" },
      }),
    ),
  );
  const { result } = renderHook(() => useMe(), { wrapper });
  await waitFor(() => expect(result.current.isSuccess).toBe(true));
  expect(result.current.data).toBeNull();
  vi.unstubAllGlobals();
});

test("useRegister surfaces the API error detail", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ title: "Conflict", status: 409, detail: "email taken" }), {
        status: 409,
        headers: { "Content-Type": "application/problem+json" },
      }),
    ),
  );
  const { result } = renderHook(() => useRegister(), { wrapper });
  result.current.mutate({ email: "a@b.pl", displayName: "Mat", password: "hunter22" });
  await waitFor(() => expect(result.current.isError).toBe(true));
  expect(result.current.error?.message).toBe("email taken");
  vi.unstubAllGlobals();
});

test("useResetPassword succeeds on 204", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(null, { status: 204 })));
  const { result } = renderHook(() => useResetPassword(), { wrapper });
  result.current.mutate({ token: "t0k3n", newPassword: "hunter22" });
  await waitFor(() => expect(result.current.isSuccess).toBe(true));
  vi.unstubAllGlobals();
});

test("useLogout navigates to /login and invalidates the whole cache", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(null, { status: 204 })));
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const invalidate = vi.spyOn(qc, "invalidateQueries");
  const { result } = renderHook(() => useLogout(), {
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    ),
  });
  result.current.mutate();
  await waitFor(() => expect(mockNavigate).toHaveBeenCalledWith({ to: "/login" }));
  expect(invalidate).toHaveBeenCalledWith();
  vi.unstubAllGlobals();
});
