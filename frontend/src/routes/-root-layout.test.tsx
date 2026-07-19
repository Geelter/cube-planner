import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { RootLayout } from "./__root";

beforeEach(() => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response("{}", { status: 401 })),
  );
  vi.stubEnv("DEV", false);
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

async function renderShell() {
  const rootRoute = createRootRoute({ component: RootLayout });
  const paths = ["/", "/cards", "/cubes", "/events", "/login"];
  const children = paths.map((path) =>
    createRoute({ getParentRoute: () => rootRoute, path, component: () => null }),
  );
  const router = createRouter({
    routeTree: rootRoute.addChildren(children),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const result = render(
    <QueryClientProvider client={qc}>
      {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
      <RouterProvider router={router as any} />
    </QueryClientProvider>,
  );
  // TanStack Router resolves the initial route match asynchronously; without
  // this, the shell (and its nav links) hasn't mounted yet when assertions
  // run. Same pattern as src/features/auth/components/a11y.test.tsx.
  await router.load();
  return result;
}

test("hamburger opens the drawer with nav links", async () => {
  await renderShell();
  // Desktop nav renders one copy; drawer is closed so no second copy.
  expect(screen.getAllByRole("link", { name: "Cards" })).toHaveLength(1);
  await userEvent.click(screen.getByRole("button", { name: "Menu" }));
  const drawer = screen.getByRole("dialog");
  expect(within(drawer).getByRole("link", { name: "Cards" })).toBeInTheDocument();
  expect(within(drawer).getByRole("link", { name: "Events" })).toBeInTheDocument();
  expect(within(drawer).getByRole("link", { name: "Log in" })).toBeInTheDocument();
});

test("drawer closes on navigation", async () => {
  await renderShell();
  await userEvent.click(screen.getByRole("button", { name: "Menu" }));
  const drawer = screen.getByRole("dialog");
  await userEvent.click(within(drawer).getByRole("link", { name: "Cards" }));
  await waitFor(() => expect(screen.getAllByRole("link", { name: "Cards" })).toHaveLength(1));
});
