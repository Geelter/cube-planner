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

import { CommitConflictError } from "../api";
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
  // "+1" and "Sol Ring" now also appear in the mobile summary bar once dirty —
  // scope to the pending panel (an <aside>, i.e. the "complementary" landmark).
  await waitFor(() =>
    expect(within(screen.getByRole("complementary")).getByText(/\+1/)).toBeDefined(),
  );
  expect(within(screen.getByRole("complementary")).getByText("Sol Ring")).toBeDefined();

  fireEvent.click(
    within(screen.getByRole("complementary")).getByRole("button", { name: /save changes/i }),
  );
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
  // The mobile summary bar mirrors the total once dirty — scope to the panel.
  await waitFor(() =>
    expect(within(screen.getByRole("complementary")).getByText(/−1/)).toBeDefined(),
  );

  fireEvent.click(
    within(screen.getByRole("complementary")).getByRole("button", { name: /save changes/i }),
  );
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
  await waitFor(() =>
    expect(within(screen.getByRole("complementary")).getByText(/\+1/)).toBeDefined(),
  );
  fireEvent.click(
    within(screen.getByRole("complementary")).getByRole("button", { name: /save changes/i }),
  );

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
  await waitFor(() =>
    expect(within(screen.getByRole("complementary")).getByText(/−1/)).toBeDefined(),
  );
  decrease();
  await waitFor(() =>
    expect(within(screen.getByRole("complementary")).getByText(/−2/)).toBeDefined(),
  );
  // Preview drops the fully-removed card.
  expect(screen.queryByRole("button", { name: /decrease quantity of lightning bolt/i })).toBeNull();

  fireEvent.click(
    within(screen.getByRole("complementary")).getByRole("button", { name: /save changes/i }),
  );
  expect(mocks.mutate).toHaveBeenCalledWith(
    expect.objectContaining({
      expectedVersion: 3,
      adds: [],
      removes: [{ oracleId: "o-bolt", quantity: 2 }],
    }),
    expect.anything(),
  );
});

// #30: below lg the editor shows a fixed summary bar while dirty. CSS
// isn't loaded in unit tests, so visibility classes (hidden/lg:) are not
// asserted here — mount/unmount and wiring are.
test("mobile bar appears when dirty and saves directly", async () => {
  render(<CubeEditorPage />);
  expect(screen.queryByRole("region", { name: /pending changes/i })).toBeNull();
  fireEvent.click(screen.getByText("pick sol ring"));
  const bar = await screen.findByRole("region", { name: /pending changes/i });
  fireEvent.click(within(bar).getByRole("button", { name: /save changes/i }));
  expect(mocks.mutate).toHaveBeenCalledWith(
    expect.objectContaining({
      expectedVersion: 3,
      adds: [{ scryfallId: "s-ring", quantity: 1 }],
      removes: [],
    }),
    expect.anything(),
  );
});

test("tapping the bar summary opens the sheet with the full panel", async () => {
  render(<CubeEditorPage />);
  fireEvent.click(screen.getByText("pick sol ring"));
  const bar = await screen.findByRole("region", { name: /pending changes/i });
  fireEvent.click(within(bar).getByRole("button", { name: /review pending changes/i }));
  const sheet = screen.getByRole("dialog", { name: /pending changes/i });
  expect(within(sheet).getByText("Sol Ring")).toBeDefined();
  expect(within(sheet).getByRole("button", { name: /save changes/i })).toBeDefined();
});

test("discard in the sheet hides the bar and empties the sheet", async () => {
  render(<CubeEditorPage />);
  fireEvent.click(screen.getByText("pick sol ring"));
  const bar = await screen.findByRole("region", { name: /pending changes/i });
  fireEvent.click(within(bar).getByRole("button", { name: /review pending changes/i }));
  const sheet = screen.getByRole("dialog", { name: /pending changes/i });
  fireEvent.click(within(sheet).getByRole("button", { name: /discard/i }));
  await waitFor(() =>
    expect(screen.queryByRole("region", { name: /pending changes/i })).toBeNull(),
  );
  // Sheet closed too — its children unmount.
  expect(within(sheet).queryByText("Sol Ring")).toBeNull();
});

// Bug: while the sheet is open, everything outside the modal <dialog> is
// inert (excluded from the a11y tree), so a conflict/error Alert rendered
// only in the page flow never gets seen or announced. The alert must also
// render inside the sheet.
test("commit conflict from the sheet surfaces the conflict alert inside the sheet dialog", async () => {
  mocks.refetch.mockResolvedValue({ data: { version: 4, cards: [] } });
  mocks.mutate.mockImplementation(
    (_vars: unknown, opts: { onError: (err: unknown) => Promise<void> | void }) =>
      opts.onError(new CommitConflictError("conflict")),
  );

  render(<CubeEditorPage />);
  fireEvent.click(screen.getByText("pick sol ring"));
  const bar = await screen.findByRole("region", { name: /pending changes/i });
  fireEvent.click(within(bar).getByRole("button", { name: /review pending changes/i }));
  const sheet = screen.getByRole("dialog", { name: /pending changes/i });

  fireEvent.click(within(sheet).getByRole("button", { name: /save changes/i }));

  await waitFor(() => expect(within(sheet).getByText(/changed elsewhere/i)).toBeDefined());
});
