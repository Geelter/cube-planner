import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createRootRoute,
  createRoute,
  createRouter,
  createMemoryHistory,
  RouterProvider,
} from "@tanstack/react-router";
import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { CubeBrowserPage } from "./CubeBrowserPage";

afterEach(() => vi.unstubAllGlobals());

// Minimal router shell: Link in CubeListItem needs a RouterProvider.
function renderWithRouter(ui: () => React.ReactElement) {
  const rootRoute = createRootRoute();
  const index = createRoute({ getParentRoute: () => rootRoute, path: "/", component: ui });
  const detail = createRoute({
    getParentRoute: () => rootRoute,
    path: "/cubes/$cubeId",
    component: () => null,
  });
  const newCube = createRoute({
    getParentRoute: () => rootRoute,
    path: "/cubes/new",
    component: () => null,
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([index, detail, newCube]),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
      <RouterProvider router={router as any} />
    </QueryClientProvider>,
  );
}

test("renders cubes from the API", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          cubes: [
            {
              id: "c1",
              name: "Vintage Cube",
              description: "The classic",
              ownerName: "Mat",
              cardCount: 540,
              visibility: "public",
              updatedAt: "2026-07-12T10:00:00Z",
            },
          ],
          total: 1,
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    ),
  );
  renderWithRouter(() => <CubeBrowserPage />);
  await waitFor(() => expect(screen.getByText("Vintage Cube")).toBeDefined());
  expect(screen.getByText(/540/)).toBeDefined();
});

test("shows empty state", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ cubes: [], total: 0 }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    ),
  );
  renderWithRouter(() => <CubeBrowserPage />);
  await waitFor(() => expect(screen.getByText(/no cubes/i)).toBeDefined());
});
