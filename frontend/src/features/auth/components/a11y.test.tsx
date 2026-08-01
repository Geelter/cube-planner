// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { cleanup, render, waitFor } from "@testing-library/react";
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
      expect(await axe(container)).toHaveNoViolations();
    });
  }

  // /account is behind requireAuth: with the suite's always-401 stub it would
  // redirect to /login (already covered above), so render it authenticated.
  it("/account", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: Request | string) => {
        const url = typeof input === "string" ? input : input.url;
        if (url.includes("/api/me")) {
          return new Response(
            JSON.stringify({
              id: "u1",
              email: "x@y",
              displayName: "X",
              providers: [],
              role: "user",
            }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          );
        }
        return new Response("{}", { status: 401 });
      }),
    );
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const router = createRouter({
      routeTree,
      history: createMemoryHistory({ initialEntries: ["/account"] }),
      context: { queryClient: qc },
    });
    const { container } = render(
      <QueryClientProvider client={qc}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );
    await router.load();
    await waitFor(() => expect(container.textContent).toContain("Account"));
    expect(await axe(container)).toHaveNoViolations();
  });
});
