// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { cleanup, render } from "@testing-library/react";
import { axe } from "vitest-axe";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { routeTree } from "@/routeTree.gen";

beforeEach(() => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: Request | string) => {
      const url = typeof input === "string" ? input : input.url;
      if (url.includes("/api/cubes")) {
        return new Response(JSON.stringify({ cubes: [], total: 0 }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
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
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: [path] }),
  });
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
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
  expect(await axe(await renderRoute("/cubes/new"))).toHaveNoViolations();
});
