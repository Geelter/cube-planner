import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import { CollectionPage } from "./CollectionPage";

afterEach(() => vi.unstubAllGlobals());

function renderPage() {
  const rootRoute = createRootRoute();
  const index = createRoute({
    getParentRoute: () => rootRoute,
    path: "/",
    component: CollectionPage,
  });
  const login = createRoute({
    getParentRoute: () => rootRoute,
    path: "/login",
    component: () => null,
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([index, login]),
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

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

test("renders items with stats and printing line", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(jsonResponse({ items: [item], total: 1, totalCopies: 4 })),
  );
  renderPage();
  expect(await screen.findByText("Lightning Bolt")).toBeInTheDocument();
  expect(screen.getByText(/Magic 2010/)).toBeInTheDocument();
  expect(screen.getByText("4")).toBeInTheDocument();
});

test("remove button PUTs quantity 0", async () => {
  // openapi-fetch may call fetch(Request) or fetch(url, init) — handle both.
  const callMethod = (input: Request | string, init?: RequestInit) =>
    init?.method ?? (typeof input === "string" ? "GET" : input.method);
  const fetchMock = vi.fn(async (input: Request | string, init?: RequestInit) => {
    if (callMethod(input, init) === "PUT") return jsonResponse({ item: null });
    return jsonResponse({ items: [item], total: 1, totalCopies: 4 });
  });
  vi.stubGlobal("fetch", fetchMock);
  renderPage();
  await userEvent.click(await screen.findByRole("button", { name: "Remove Lightning Bolt" }));
  await waitFor(async () => {
    const put = fetchMock.mock.calls.find(
      ([input, init]) =>
        callMethod(input as Request | string, init as RequestInit | undefined) === "PUT",
    );
    expect(put).toBeDefined();
    const [input, init] = put as [Request | string, RequestInit | undefined];
    const rawBody =
      init?.body ?? (typeof input === "string" ? undefined : await input.clone().text());
    expect(JSON.parse(rawBody as string)).toEqual({ quantity: 0 });
  });
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
