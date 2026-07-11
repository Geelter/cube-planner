// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { cleanup, render } from "@testing-library/react";
import { axe } from "vitest-axe";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { routeTree } from "@/routeTree.gen";

const PATHS = [
  "/",
  "/login",
  "/login?error=oauth",
  "/register",
  "/forgot-password",
  "/reset-password?token=t",
  "/verify-email?token=t",
  "/account",
];

describe("auth screens have no axe violations", () => {
  beforeEach(() => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response("{}", { status: 401 })),
    );
    // The root layout only mounts TanStackDevtools in DEV. Under vitest,
    // import.meta.env.DEV defaults to true, so every render in this suite
    // would mount/unmount the devtools panel — which is unrelated to the
    // a11y contract under test and has a known mount/unmount race that
    // throws ("Devtools is not mounted") when torn down quickly across
    // repeated renders. Force it off for this suite.
    vi.stubEnv("DEV", false);
  });

  // This test suite renders a full RouterProvider tree (header + main) per
  // path. Without explicit cleanup between renders, prior renders' DOM
  // (including <header>/<main> landmarks) stays mounted, causing axe to
  // report false-positive "duplicate landmark" violations against leftover
  // nodes from earlier test cases.
  afterEach(() => {
    cleanup();
  });

  for (const path of PATHS) {
    it(path, async () => {
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
      expect(await axe(container)).toHaveNoViolations();
    });
  }
});
