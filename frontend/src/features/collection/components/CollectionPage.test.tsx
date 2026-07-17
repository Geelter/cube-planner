import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import { CollectionPage } from "./CollectionPage";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

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

test("clamps back to page 0 when a non-first page empties out", async () => {
  // openapi-fetch may call fetch(Request) or fetch(url, init) — handle both.
  const requestUrl = (input: Request | string) => (typeof input === "string" ? input : input.url);
  const fetchMock = vi.fn(async (input: Request | string) => {
    const url = new URL(requestUrl(input), "http://localhost");
    const offset = url.searchParams.get("offset");
    if (offset === "50") return jsonResponse({ items: [], total: 0, totalCopies: 0 });
    return jsonResponse({ items: [item], total: 51, totalCopies: 51 });
  });
  vi.stubGlobal("fetch", fetchMock);
  renderPage();

  expect(await screen.findByText("Lightning Bolt")).toBeInTheDocument();
  await userEvent.click(await screen.findByRole("button", { name: "Next page" }));

  // The empty page-2 response briefly renders the empty state...
  await waitFor(() => {
    expect(fetchMock.mock.calls.some(([input]) => requestUrl(input).includes("offset=50"))).toBe(
      true,
    );
  });

  // ...then the page clamps back to 0 and the real content reappears.
  expect(await screen.findByText("Lightning Bolt")).toBeInTheDocument();
  expect(screen.queryByText(/collection is empty|no cards match/i)).not.toBeInTheDocument();
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

test("failed quantity update surfaces an error alert", async () => {
  const callMethod = (input: Request | string, init?: RequestInit) =>
    init?.method ?? (typeof input === "string" ? "GET" : input.method);
  const fetchMock = vi.fn(async (input: Request | string, init?: RequestInit) => {
    if (callMethod(input, init) === "PUT") {
      return jsonResponse(
        { title: "Internal", status: 500, detail: "quantity update failed" },
        500,
      );
    }
    return jsonResponse({ items: [item], total: 1, totalCopies: 4 });
  });
  vi.stubGlobal("fetch", fetchMock);
  renderPage();
  await userEvent.click(await screen.findByRole("button", { name: "Remove Lightning Bolt" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("quantity update failed");
  // The list must resync after a failure, or the UI keeps showing state
  // the server rejected.
  await waitFor(() => {
    const gets = fetchMock.mock.calls.filter(
      ([input, init]) =>
        callMethod(input as Request | string, init as RequestInit | undefined) === "GET",
    );
    expect(gets.length).toBeGreaterThan(1);
  });
});

test("row actions disable while a quantity mutation is in flight", async () => {
  const callMethod = (input: Request | string, init?: RequestInit) =>
    init?.method ?? (typeof input === "string" ? "GET" : input.method);
  let resolvePut!: (r: Response) => void;
  const putGate = new Promise<Response>((r) => {
    resolvePut = r;
  });
  const fetchMock = vi.fn(async (input: Request | string, init?: RequestInit) => {
    if (callMethod(input, init) === "PUT") return putGate;
    return jsonResponse({ items: [item], total: 1, totalCopies: 4 });
  });
  vi.stubGlobal("fetch", fetchMock);
  renderPage();
  const remove = await screen.findByRole("button", { name: "Remove Lightning Bolt" });
  await userEvent.click(remove);
  await waitFor(() => expect(remove).toBeDisabled());
  resolvePut(jsonResponse({ item: null }));
  await waitFor(() => expect(remove).not.toBeDisabled());
});

test("search input enforces the API's 100-char limit", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(jsonResponse({ items: [item], total: 1, totalCopies: 4 })),
  );
  renderPage();
  const input = await screen.findByLabelText(/search/i);
  // The backend rejects search params over 100 chars with a 422; the
  // input must not let the user type past it.
  expect(input).toHaveAttribute("maxlength", "100");
});
