import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { WantlistPage } from "./WantlistPage";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function renderPage() {
  const rootRoute = createRootRoute();
  const wantlist = createRoute({
    getParentRoute: () => rootRoute,
    path: "/cubes/$cubeId/wantlist",
    component: WantlistPage,
  });
  const login = createRoute({
    getParentRoute: () => rootRoute,
    path: "/login",
    component: () => null,
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([wantlist, login]),
    history: createMemoryHistory({ initialEntries: ["/cubes/c1/wantlist"] }),
  });
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
      <RouterProvider router={router as any} />
    </QueryClientProvider>,
  );
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

test("renders missing cards with quantities and the download button", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      jsonResponse({
        cubeName: "Vintage Cube",
        totalMissing: 2,
        items: [
          {
            oracleId: "o1",
            scryfallId: "s1",
            name: "Lightning Bolt",
            manaCost: "{R}",
            imageSmall: null,
            imageNormal: null,
            missingQuantity: 1,
            cubeQuantity: 4,
            ownedQuantity: 3,
          },
        ],
      }),
    ),
  );
  renderPage();
  expect(await screen.findByText("Lightning Bolt")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Download for Cardmarket" })).toBeInTheDocument();
  const row = screen.getByText("Lightning Bolt").closest("tr");
  expect(row).toHaveTextContent("1");
  expect(row).toHaveTextContent("4");
  expect(row).toHaveTextContent("3");
});

test("empty wantlist shows the own-everything state, no download", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(jsonResponse({ cubeName: "Vintage Cube", totalMissing: 0, items: [] })),
  );
  renderPage();
  expect(await screen.findByText("You own everything in this cube.")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Download for Cardmarket" })).not.toBeInTheDocument();
});

test("shows a login prompt on 401", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(jsonResponse({ title: "Unauthorized", status: 401 }, 401)),
  );
  renderPage();
  // The alert text and the nested link both contain "Log in", so assert on
  // the more specific link role rather than a fuzzy text match.
  expect(await screen.findByRole("link", { name: /log in/i })).toBeInTheDocument();
});
