import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createRootRoute,
  createRoute,
  createRouter,
  createMemoryHistory,
  RouterProvider,
} from "@tanstack/react-router";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import { m } from "@/paraglide/messages";
import { CubeBrowserPage } from "./CubeBrowserPage";

const auth: { me: { id: string; displayName: string } | null } = { me: null };
vi.mock("@/features/auth/api", () => ({ useMe: () => ({ data: auth.me }) }));

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  auth.me = null;
});

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

test("hides the new cube button when logged out", async () => {
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
  expect(screen.queryByRole("link", { name: /new cube/i })).toBeNull();
});

test("shows the new cube button when logged in", async () => {
  auth.me = { id: "u1", displayName: "Mat" };
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
  await waitFor(() => expect(screen.getByRole("link", { name: /new cube/i })).toBeInTheDocument());
});

test("pagination buttons spin while the next page loads", async () => {
  const pageOne = () =>
    new Response(
      JSON.stringify({
        cubes: [
          {
            id: "c1",
            name: "Vintage Cube",
            description: "",
            ownerName: "Mat",
            cardCount: 540,
            visibility: "public",
            updatedAt: "2026-07-12T10:00:00Z",
          },
        ],
        total: 21, // > CUBES_PAGE_SIZE → pagination renders
      }),
      { status: 200, headers: { "Content-Type": "application/json" } },
    );
  const fetchMock = vi
    .fn()
    .mockResolvedValueOnce(pageOne())
    .mockImplementation(() => new Promise(() => {})); // page 2 never resolves
  vi.stubGlobal("fetch", fetchMock);
  renderWithRouter(() => <CubeBrowserPage />);
  await waitFor(() => expect(screen.getByText("Vintage Cube")).toBeDefined());
  const next = screen.getByRole("button", { name: m.pagination_next() });
  await userEvent.click(next);
  await waitFor(() => expect(next.getAttribute("aria-busy")).toBe("true"));
  // keepPreviousData: page 1 content stays visible while spinning.
  expect(screen.getByText("Vintage Cube")).toBeDefined();
});
