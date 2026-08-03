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

// Same pattern as CubeBrowserPage.test.tsx: mock useMe so tests can flip
// between guest (null) and signed-in. useLogout is mocked too because
// RootLayout renders its logout buttons off it.
const auth: { me: { id: string; displayName: string } | null } = { me: null };
vi.mock("@/features/auth/api", () => ({
  useMe: () => ({ data: auth.me }),
  useLogout: () => ({ isPending: false, mutate: vi.fn() }),
}));

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
  auth.me = null;
});

async function renderShell() {
  const rootRoute = createRootRoute({ component: RootLayout });
  const paths = [
    "/",
    "/cards",
    "/cubes",
    "/events",
    "/collection",
    "/login",
    "/cubes/mine",
    "/account",
  ];
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

test("primary destinations live in the bottom nav; drawer keeps secondary items", async () => {
  await renderShell();
  const bottomNav = screen.getByRole("navigation", { name: "Primary" });
  for (const name of ["Cards", "Cubes", "Events", "Collection"]) {
    expect(within(bottomNav).getByRole("link", { name })).toBeInTheDocument();
  }
  await userEvent.click(screen.getByRole("button", { name: "Menu" }));
  const drawer = screen.getByRole("dialog");
  for (const name of ["Cards", "Cubes", "Events", "Collection"]) {
    expect(within(drawer).queryByRole("link", { name })).toBeNull();
  }
  expect(within(drawer).getByRole("link", { name: "Log in" })).toBeInTheDocument();
});

test("signed-in drawer keeps My cubes/account/logout but not Collection", async () => {
  auth.me = { id: "user-1", displayName: "Ada Lovelace" };
  await renderShell();
  await userEvent.click(screen.getByRole("button", { name: "Menu" }));
  const drawer = screen.getByRole("dialog");
  for (const name of ["Cards", "Cubes", "Events", "Collection"]) {
    expect(within(drawer).queryByRole("link", { name })).toBeNull();
  }
  expect(within(drawer).getByRole("link", { name: "My cubes" })).toBeInTheDocument();
  expect(within(drawer).getByRole("link", { name: "Ada Lovelace" })).toBeInTheDocument();
  expect(within(drawer).getByRole("button", { name: "Log out" })).toBeInTheDocument();
});

test("drawer closes on navigation", async () => {
  await renderShell();
  await userEvent.click(screen.getByRole("button", { name: "Menu" }));
  const drawer = screen.getByRole("dialog");
  await userEvent.click(within(drawer).getByRole("link", { name: "Log in" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  // Cards remains available via the desktop top nav and the bottom tab bar.
  expect(screen.getAllByRole("link", { name: "Cards" })).toHaveLength(2);
});

// #23: tapping the drawer link for the route you are already on doesn't
// change the pathname, so the pathname-keyed close effect never fires —
// the delegated click handler must close the drawer anyway.
test("drawer closes when tapping the current route's link", async () => {
  await renderShell();
  // Navigate to /login via the top nav first.
  await userEvent.click(screen.getByRole("link", { name: "Log in" }));
  await userEvent.click(screen.getByRole("button", { name: "Menu" }));
  const drawer = screen.getByRole("dialog");
  await userEvent.click(within(drawer).getByRole("link", { name: "Log in" }));
  // Drawer children unmount on close, so only the top-bar copy remains.
  await waitFor(() => expect(screen.getAllByRole("link", { name: "Log in" })).toHaveLength(1));
  // Cards remains available via the desktop top nav and the bottom tab bar.
  expect(screen.getAllByRole("link", { name: "Cards" })).toHaveLength(2);
});
