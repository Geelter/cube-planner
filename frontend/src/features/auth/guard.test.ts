import { QueryClient } from "@tanstack/react-query";
import { isRedirect } from "@tanstack/react-router";
import { afterEach, expect, test, vi } from "vitest";
import { requireAuth } from "./guard";

afterEach(() => vi.unstubAllGlobals());

test("redirects to /login when there is no session", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ title: "Unauthorized", status: 401 }), {
        status: 401,
        headers: { "Content-Type": "application/problem+json" },
      }),
    ),
  );
  const queryClient = new QueryClient();
  const thrown: unknown = await requireAuth({ context: { queryClient } }).then(
    () => null,
    (e: unknown) => e,
  );
  expect(thrown).not.toBeNull();
  expect(isRedirect(thrown)).toBe(true);
});

test("passes when a session exists", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ id: "u1", displayName: "Mat", email: "m@x.pl" }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    ),
  );
  const queryClient = new QueryClient();
  await expect(requireAuth({ context: { queryClient } })).resolves.toBeUndefined();
});
