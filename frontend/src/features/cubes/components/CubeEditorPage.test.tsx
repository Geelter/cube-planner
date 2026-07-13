import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";

type BlockerOptions = { shouldBlockFn: () => boolean; disabled?: boolean };

const mocks = vi.hoisted(() => ({
  useParams: vi.fn(() => ({ cubeId: "cube-1" })),
  navigate: vi.fn(),
  mutate: vi.fn(),
  refetch: vi.fn(),
  // Latest useBlocker options, so tests can simulate the router consulting
  // the blocker registered on the render that initiated a navigation.
  blockerOptions: {
    current: null as null | BlockerOptions,
  },
}));

vi.mock("@tanstack/react-router", () => ({
  getRouteApi: () => ({
    useParams: mocks.useParams,
    useSearch: () => ({}),
  }),
  useNavigate: () => mocks.navigate,
  useBlocker: (options: BlockerOptions) => {
    mocks.blockerOptions.current = options;
  },
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

vi.mock("./CubeSettingsSection", () => ({
  CubeSettingsSection: () => <div>Settings section (mocked)</div>,
}));

import { CubeEditorPage } from "./CubeEditorPage";

beforeEach(() => {
  mocks.mutate.mockReset();
  mocks.navigate.mockReset();
  mocks.blockerOptions.current = null;
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

// Regression: saving navigates before the pending reset re-renders, so the
// blocker from the previous render is still active — the navigate must opt
// out via ignoreBlocker or the user gets the unsaved-changes confirm.
test("saving does not trigger the unsaved-changes blocker", async () => {
  // happy-dom has no window.confirm; stub it so shouldBlockFn can run.
  const confirmSpy = vi.fn(() => true);
  vi.stubGlobal("confirm", confirmSpy);
  // Simulate the router: consult the currently registered blocker unless the
  // navigation carries ignoreBlocker.
  mocks.navigate.mockImplementation((opts: { ignoreBlocker?: boolean }) => {
    const blocker = mocks.blockerOptions.current;
    if (!opts.ignoreBlocker && blocker && !blocker.disabled) blocker.shouldBlockFn();
    return Promise.resolve();
  });
  mocks.mutate.mockImplementation(
    (_vars: unknown, opts: { onSuccess: (result: unknown) => void }) =>
      opts.onSuccess({ version: 4 }),
  );

  render(<CubeEditorPage />);
  fireEvent.click(screen.getByText("pick sol ring"));
  await waitFor(() => expect(screen.getByText(/\+1/)).toBeDefined());
  fireEvent.click(screen.getByRole("button", { name: /save changes/i }));

  expect(mocks.navigate).toHaveBeenCalledWith(
    expect.objectContaining({ to: "/cubes/$cubeId", ignoreBlocker: true }),
  );
  expect(confirmSpy).not.toHaveBeenCalled();
  vi.unstubAllGlobals();
});

// Regression: stepper dispatches must carry the true server entry, not the
// preview entry whose quantity has pending removes baked in — otherwise the
// reducer's remove cap makes the second decrement of a 2-copy card a no-op.
test("repeated decrement removes the full server quantity of a multi-copy card", async () => {
  render(<CubeEditorPage />);
  const decrease = () =>
    fireEvent.click(screen.getByRole("button", { name: /decrease quantity of lightning bolt/i }));

  decrease();
  await waitFor(() => expect(screen.getByText(/−1/)).toBeDefined());
  decrease();
  await waitFor(() => expect(screen.getByText(/−2/)).toBeDefined());
  // Preview drops the fully-removed card.
  expect(screen.queryByRole("button", { name: /decrease quantity of lightning bolt/i })).toBeNull();

  fireEvent.click(screen.getByRole("button", { name: /save changes/i }));
  expect(mocks.mutate).toHaveBeenCalledWith(
    expect.objectContaining({
      expectedVersion: 3,
      adds: [],
      removes: [{ oracleId: "o-bolt", quantity: 2 }],
    }),
    expect.anything(),
  );
});
