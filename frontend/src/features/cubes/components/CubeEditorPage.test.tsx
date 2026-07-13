import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  useParams: vi.fn(() => ({ cubeId: "cube-1" })),
  navigate: vi.fn(),
  mutate: vi.fn(),
  refetch: vi.fn(),
}));

vi.mock("@tanstack/react-router", () => ({
  getRouteApi: () => ({
    useParams: mocks.useParams,
    useSearch: () => ({}),
  }),
  useNavigate: () => mocks.navigate,
  useBlocker: vi.fn(),
}));

vi.mock("../api", async (importOriginal) => {
  const original = await importOriginal<typeof import("../api")>();
  return {
    ...original,
    useCube: () => ({
      isPending: false,
      isError: false,
      data: {
        id: "cube-1",
        name: "Test Cube",
        version: 3,
        ownerName: "Mat",
        cardCount: 1,
        description: "",
        visibility: "public",
      },
    }),
    useCubeCards: () => ({
      isPending: false,
      isError: false,
      data: {
        version: 3,
        cards: [
          {
            scryfallId: "s-bolt",
            oracleId: "o-bolt",
            name: "Lightning Bolt",
            manaCost: "{R}",
            typeLine: "Instant",
            cmc: 1,
            colors: ["R"],
            colorIdentity: ["R"],
            rarity: "common",
            imageSmall: null,
            imageNormal: null,
            quantity: 2,
          },
        ],
      },
      refetch: mocks.refetch,
    }),
    useCommitChange: () => ({
      mutate: mocks.mutate,
      isPending: false,
      isError: false,
      error: null,
    }),
  };
});

vi.mock("@/shared/cards/CardAutocomplete", () => ({
  CardAutocomplete: ({ onSelect }: { onSelect: (c: unknown) => void }) => (
    <button
      type="button"
      onClick={() =>
        onSelect({
          scryfallId: "s-ring",
          oracleId: "o-ring",
          name: "Sol Ring",
          manaCost: "{1}",
          typeLine: "Artifact",
          imageSmall: null,
        })
      }
    >
      pick sol ring
    </button>
  ),
}));

import { CubeEditorPage } from "./CubeEditorPage";

beforeEach(() => {
  mocks.mutate.mockReset();
});

afterEach(() => {
  cleanup();
});

test("add via autocomplete lands in pending and commits with expectedVersion", async () => {
  render(<CubeEditorPage />);
  fireEvent.click(screen.getByText("pick sol ring"));
  await waitFor(() => expect(screen.getByText(/\+1/)).toBeDefined());
  // "Sol Ring" now appears twice (optimistic preview list + pending panel) —
  // scope to the pending panel (an <aside>, i.e. the "complementary" landmark).
  expect(within(screen.getByRole("complementary")).getByText("Sol Ring")).toBeDefined();

  fireEvent.click(screen.getByRole("button", { name: /save changes/i }));
  expect(mocks.mutate).toHaveBeenCalledWith(
    expect.objectContaining({
      expectedVersion: 3,
      adds: [{ scryfallId: "s-ring", quantity: 1 }],
      removes: [],
    }),
    expect.anything(),
  );
});

test("decrement of existing card lands in pending removes", async () => {
  render(<CubeEditorPage />);
  fireEvent.click(screen.getByRole("button", { name: /decrease quantity of lightning bolt/i }));
  await waitFor(() => expect(screen.getByText(/−1/)).toBeDefined());

  fireEvent.click(screen.getByRole("button", { name: /save changes/i }));
  expect(mocks.mutate).toHaveBeenCalledWith(
    expect.objectContaining({
      expectedVersion: 3,
      adds: [],
      removes: [{ oracleId: "o-bolt", quantity: 1 }],
    }),
    expect.anything(),
  );
});

test("save disabled with no pending changes", () => {
  render(<CubeEditorPage />);
  const saveButton = screen.getByRole("button", { name: /save changes/i });
  expect(saveButton.hasAttribute("disabled")).toBe(true);
});
