import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { CreateCubePage } from "./CreateCubePage";

const mocks = vi.hoisted(() => ({ navigate: vi.fn() }));

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => mocks.navigate,
}));

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <CreateCubePage />
    </QueryClientProvider>,
  );
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

const match = (scryfallId: string, oracleId: string, name: string) => ({
  scryfallId,
  oracleId,
  name,
  manaCost: "{U}",
  typeLine: "Instant",
  setCode: "tst",
  setName: "Test Set",
  collectorNumber: "1",
  imageSmall: null,
  imageNormal: null,
});

beforeEach(() => mocks.navigate.mockReset());
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

test("a pasted list is resolved before the cube is created", async () => {
  const calls: string[] = [];
  const fetchMock = vi.fn(async (input: Request | string) => {
    const url = typeof input === "string" ? input : input.url;
    if (url.includes("/cards/resolve-list")) {
      calls.push("resolve-list");
      return jsonResponse({
        lines: [
          {
            lineNumber: 1,
            raw: "4 Brainstorm",
            quantity: 4,
            status: "matched",
            match: match("s-storm", "o-storm", "Brainstorm"),
          },
        ],
      });
    }
    if (url.endsWith("/api/cubes")) {
      calls.push("create-cube");
      return jsonResponse({
        id: "cube-9",
        name: "My Cube",
        version: 1,
        ownerName: "Mat",
        cardCount: 0,
        description: "",
        visibility: "public",
      });
    }
    return jsonResponse({});
  });
  vi.stubGlobal("fetch", fetchMock);

  renderPage();
  await userEvent.type(screen.getByLabelText("Name"), "My Cube");
  await userEvent.type(screen.getByLabelText(/cards/i), "4 Brainstorm");
  await userEvent.click(screen.getByRole("button", { name: "Create cube" }));

  await userEvent.click(await screen.findByRole("button", { name: /add to collection/i }));

  await waitFor(() => expect(mocks.navigate).toHaveBeenCalled());
  // resolve-list must complete before /api/cubes is ever hit — a hopeless
  // list must never leave an orphan cube behind.
  expect(calls).toEqual(["resolve-list", "create-cube"]);
  expect(mocks.navigate).toHaveBeenCalledWith({
    to: "/cubes/$cubeId/edit",
    params: { cubeId: "cube-9" },
    state: {
      importedItems: [
        {
          card: expect.objectContaining({ name: "Brainstorm", scryfallId: "s-storm" }),
          quantity: 4,
        },
      ],
    },
  });
});

test("a hopeless list does not create a cube", async () => {
  const fetchMock = vi.fn(async (input: Request | string) => {
    const url = typeof input === "string" ? input : input.url;
    if (url.includes("/cards/resolve-list")) {
      return jsonResponse({
        lines: [{ lineNumber: 1, raw: "Gibberish", quantity: 1, status: "unmatched" }],
      });
    }
    return jsonResponse({});
  });
  vi.stubGlobal("fetch", fetchMock);

  renderPage();
  await userEvent.type(screen.getByLabelText("Name"), "My Cube");
  await userEvent.type(screen.getByLabelText(/cards/i), "Gibberish");
  await userEvent.click(screen.getByRole("button", { name: "Create cube" }));

  await screen.findByText("Not found (1)");
  await userEvent.click(screen.getByRole("button", { name: "Back" }));

  const createCalls = fetchMock.mock.calls.filter(([input]) => {
    const url = typeof input === "string" ? input : (input as Request).url;
    return url.endsWith("/api/cubes");
  });
  expect(createCalls).toHaveLength(0);
  expect(mocks.navigate).not.toHaveBeenCalled();
});

test("an empty card list creates the cube directly, unchanged from before", async () => {
  const fetchMock = vi.fn(async (input: Request | string) => {
    const url = typeof input === "string" ? input : input.url;
    if (url.endsWith("/api/cubes")) {
      return jsonResponse({
        id: "cube-1",
        name: "My Cube",
        version: 1,
        ownerName: "Mat",
        cardCount: 0,
        description: "",
        visibility: "public",
      });
    }
    return jsonResponse({});
  });
  vi.stubGlobal("fetch", fetchMock);

  renderPage();
  await userEvent.type(screen.getByLabelText("Name"), "My Cube");
  await userEvent.click(screen.getByRole("button", { name: "Create cube" }));

  await waitFor(() =>
    expect(mocks.navigate).toHaveBeenCalledWith({
      to: "/cubes/$cubeId",
      params: { cubeId: "cube-1" },
    }),
  );
  const resolveCalls = fetchMock.mock.calls.filter(([input]) => {
    const url = typeof input === "string" ? input : (input as Request).url;
    return url.includes("/cards/resolve-list");
  });
  expect(resolveCalls).toHaveLength(0);
});
