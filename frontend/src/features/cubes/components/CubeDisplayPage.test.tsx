import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import { CubeDisplayPage } from "./CubeDisplayPage";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const cubeFixture = {
  id: "c1",
  name: "Vintage Cube",
  description: "",
  ownerId: "u1",
  ownerName: "Owner",
  cardCount: 1,
  visibility: "public",
  version: 1,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

const cardEntry = {
  scryfallId: "s1",
  oracleId: "o1",
  name: "Sol Ring",
  manaCost: "{1}",
  typeLine: "Artifact",
  cmc: 1,
  colors: [],
  colorIdentity: [],
  rarity: "uncommon",
  imageSmall: null,
  imageNormal: "https://img/s1.jpg",
  quantity: 1,
};

const printings = [
  {
    scryfallId: "s1",
    oracleId: "o1",
    name: "Sol Ring",
    manaCost: "{1}",
    typeLine: "Artifact",
    oracleText: "Add {C}{C}.",
    setName: "Commander",
    setCode: "cmd",
    collectorNumber: "1",
    rarity: "uncommon",
    releasedAt: "2011-06-17",
    cmc: 1,
    colors: [],
    colorIdentity: [],
    promo: false,
    imageSmall: "https://img/s1-small.jpg",
    imageNormal: "https://img/s1.jpg",
    backImageNormal: null,
  },
  {
    scryfallId: "s2",
    oracleId: "o1",
    name: "Sol Ring",
    manaCost: "{1}",
    typeLine: "Artifact",
    oracleText: "Add {C}{C}.",
    setName: "Alpha",
    setCode: "lea",
    collectorNumber: "2",
    rarity: "uncommon",
    releasedAt: "1993-08-05",
    cmc: 1,
    colors: [],
    colorIdentity: [],
    promo: false,
    imageSmall: null,
    imageNormal: "https://img/s2.jpg",
    backImageNormal: null,
  },
];

const owner = { id: "u1", displayName: "Owner", email: "o@x", providers: null };

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function renderCubePage({ me }: { me: typeof owner | null }) {
  const fetchMock = vi.fn(async (input: Request) => {
    const url = input.url;
    if (url.includes("/change-printing")) {
      return new Response(null, { status: 204 });
    }
    if (url.includes("/printings")) {
      return jsonResponse({ printings });
    }
    if (url.includes("/api/me")) {
      return me === null
        ? jsonResponse({ title: "Unauthorized", status: 401 }, 401)
        : jsonResponse(me);
    }
    if (url.includes("/cards")) {
      return jsonResponse({ version: 1, cards: [cardEntry] });
    }
    if (url.includes("/api/cubes/")) {
      return jsonResponse(cubeFixture);
    }
    return jsonResponse({}, 404);
  });
  vi.stubGlobal("fetch", fetchMock);

  const rootRoute = createRootRoute();
  const display = createRoute({
    getParentRoute: () => rootRoute,
    path: "/cubes/$cubeId/",
    component: CubeDisplayPage,
    validateSearch: (s: Record<string, unknown>): { atVersion?: number } => ({
      ...(typeof s.atVersion === "number" ? { atVersion: s.atVersion } : {}),
    }),
  });
  const wantlist = createRoute({
    getParentRoute: () => rootRoute,
    path: "/cubes/$cubeId/wantlist",
    component: () => null,
  });
  const history = createRoute({
    getParentRoute: () => rootRoute,
    path: "/cubes/$cubeId/history",
    component: () => null,
  });
  const edit = createRoute({
    getParentRoute: () => rootRoute,
    path: "/cubes/$cubeId/edit",
    component: () => null,
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([display, wantlist, history, edit]),
    history: createMemoryHistory({ initialEntries: ["/cubes/c1"] }),
  });
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
      <RouterProvider router={router as any} />
    </QueryClientProvider>,
  );
  return fetchMock;
}

test("activating a card row opens the preview sheet", async () => {
  renderCubePage({ me: owner });
  await userEvent.click(await screen.findByRole("button", { name: /Sol Ring/ }));
  expect(await screen.findByText("Add {C}{C}.")).toBeInTheDocument();
});

test("owner sees change printing; picking fires the mutation", async () => {
  const fetchMock = renderCubePage({ me: owner });
  await userEvent.click(await screen.findByRole("button", { name: /Sol Ring/ }));
  await userEvent.click(await screen.findByRole("button", { name: "Change printing" }));
  // PrintingPickerDialog opens on top; the non-current row (Alpha) fires
  // the mutation on click.
  await userEvent.click(await screen.findByRole("button", { name: /Alpha/ }));
  await vi.waitFor(() => {
    const changePrintingCall = fetchMock.mock.calls.find(([req]) =>
      (req as Request).url.includes("/change-printing"),
    );
    expect(changePrintingCall).toBeDefined();
    expect((changePrintingCall![0] as Request).method).toBe("POST");
  });
});

test("non-owner gets an info-only sheet", async () => {
  renderCubePage({ me: null });
  await userEvent.click(await screen.findByRole("button", { name: /Sol Ring/ }));
  await screen.findByText("Add {C}{C}.");
  expect(screen.queryByRole("button", { name: "Change printing" })).not.toBeInTheDocument();
});
