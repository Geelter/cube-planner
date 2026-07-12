import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { cleanup, render, screen } from "@testing-library/react";
import { StrictMode } from "react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { m } from "@/paraglide/messages";
import { routeTree } from "@/routeTree.gen";

// Renders the real /verify-email route under StrictMode, which double-invokes
// effects and remounts components in development. The token is single-use, so
// the page must fire the verify request exactly once — and the retained
// component instance must still observe the result. Driving the request
// through useMutation breaks the second half: the mutation fires on the
// StrictMode-discarded observer and the surviving one stays idle forever
// (TanStack/query#5341, closed wontfix), leaving the page stuck on
// "Verifying…". The page therefore tracks the request with component state.
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  fetchMock = vi.fn(async (input: Request) =>
    input.url.includes("/api/auth/verify-email")
      ? new Response(null, { status: 204 })
      : new Response("{}", { status: 401 }),
  );
  vi.stubGlobal("fetch", fetchMock);
  // Same devtools mount/unmount race as a11y.test.tsx — irrelevant here.
  vi.stubEnv("DEV", false);
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});

function renderVerifyRoute() {
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: ["/verify-email?token=abc123"] }),
  });
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <StrictMode>
      <QueryClientProvider client={qc}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </StrictMode>,
  );
  return router;
}

test("verify-email reaches the success screen under StrictMode", async () => {
  const router = renderVerifyRoute();
  await router.load();

  await screen.findByRole("heading", { name: m.verify_done_title() });
});

test("verify-email consumes the single-use token exactly once under StrictMode", async () => {
  const router = renderVerifyRoute();
  await router.load();

  await screen.findByRole("heading", { name: m.verify_done_title() });
  const verifyCalls = fetchMock.mock.calls.filter(([input]) =>
    (input as Request).url.includes("/api/auth/verify-email"),
  );
  expect(verifyCalls).toHaveLength(1);
});
