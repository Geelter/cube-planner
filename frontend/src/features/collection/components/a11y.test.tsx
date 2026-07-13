// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { cleanup, render, waitFor } from "@testing-library/react";
import { axe } from "vitest-axe";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { routeTree } from "@/routeTree.gen";

const item = {
  scryfallId: "s1",
  oracleId: "o1",
  name: "Lightning Bolt",
  manaCost: "{R}",
  typeLine: "Instant",
  setCode: "m10",
  setName: "Magic 2010",
  collectorNumber: "146",
  imageSmall: null,
  imageNormal: null,
  quantity: 4,
};

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
      if (url.includes("/wantlist")) {
        return json({
          cubeName: "Vintage Cube",
          totalMissing: 1,
          items: [{ ...item, missingQuantity: 1, cubeQuantity: 4, ownedQuantity: 3 }],
        });
      }
      if (url.includes("/api/collection")) {
        return json({ items: [item], total: 1, totalCopies: 4 });
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
  await waitFor(() => expect(container.textContent).not.toBe(""));
  return container;
}

it("collection page has no axe violations", async () => {
  const container = await renderRoute("/collection");
  await waitFor(() => expect(container.textContent).toContain("Lightning Bolt"));
  expect(await axe(container)).toHaveNoViolations();
});

it("wantlist page has no axe violations", async () => {
  const container = await renderRoute("/cubes/c1/wantlist");
  await waitFor(() => expect(container.textContent).toContain("Lightning Bolt"));
  expect(await axe(container)).toHaveNoViolations();
});
