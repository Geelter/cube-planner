// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { cleanup, render, waitFor } from "@testing-library/react";
import { axe } from "vitest-axe";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { routeTree } from "@/routeTree.gen";

beforeEach(() => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: Request | string) => {
      const url = typeof input === "string" ? input : input.url;
      const json = (body: unknown) =>
        new Response(JSON.stringify(body), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (url.includes("/api/cubes")) {
        return json({ cubes: [], total: 0 });
      }
      if (url.includes("/api/me")) {
        return json({ id: "u1", email: "x@y", displayName: "X", providers: [], role: "user" });
      }
      return new Response("{}", { status: 401 });
    }),
  );
  vi.stubEnv("DEV", false);
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

async function renderRoute(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: [path] }),
    context: { queryClient: qc },
  });
  const { container } = render(
    <QueryClientProvider client={qc}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  await router.load();
  return container;
}

it("/cubes has no axe violations", async () => {
  expect(await axe(await renderRoute("/cubes"))).toHaveNoViolations();
});

it("/cubes/new has no axe violations", async () => {
  const container = await renderRoute("/cubes/new");
  // Guarded route: make sure we axe-check CreateCubePage, not a login redirect.
  await waitFor(() => expect(container.textContent).toContain("New cube"));
  expect(await axe(container)).toHaveNoViolations();
});
