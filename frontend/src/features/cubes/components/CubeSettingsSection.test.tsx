import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  navigate: vi.fn(),
  updateMutate: vi.fn(),
  deleteMutate: vi.fn(),
}));

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => mocks.navigate,
}));

vi.mock("../api", async (importOriginal) => {
  const original = await importOriginal<typeof import("../api")>();
  return {
    ...original,
    useUpdateCube: () => ({
      mutate: mocks.updateMutate,
      isPending: false,
      isError: false,
      error: null,
    }),
    useDeleteCube: () => ({
      mutate: mocks.deleteMutate,
      isPending: false,
      isError: false,
      error: null,
    }),
  };
});

import type { CubeDetail } from "../api";
import { CubeSettingsSection } from "./CubeSettingsSection";

const cube = {
  id: "cube-1",
  name: "Test Cube",
  description: "A cube",
  visibility: "public",
  version: 3,
  ownerName: "Mat",
  cardCount: 1,
} as CubeDetail;

beforeEach(() => {
  mocks.navigate.mockReset();
  mocks.updateMutate.mockReset();
  mocks.deleteMutate.mockReset();
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

test("saving meta submits the edited fields", () => {
  render(<CubeSettingsSection cube={cube} />);
  fireEvent.change(screen.getByLabelText(/name/i), { target: { value: "Renamed" } });
  fireEvent.click(screen.getByRole("button", { name: /save details/i }));
  expect(mocks.updateMutate).toHaveBeenCalledWith({
    name: "Renamed",
    description: "A cube",
    visibility: "public",
  });
});

// Regression: the settings section lives inside the editor, whose
// unsaved-changes blocker may be active — the post-delete navigate must
// bypass it (pending edits are meaningless once the cube is gone).
test("delete confirms, then navigates with ignoreBlocker", () => {
  vi.stubGlobal(
    "confirm",
    vi.fn(() => true),
  );
  mocks.deleteMutate.mockImplementation((_vars: unknown, opts: { onSuccess: () => void }) =>
    opts.onSuccess(),
  );

  render(<CubeSettingsSection cube={cube} />);
  fireEvent.click(screen.getByRole("button", { name: /delete cube/i }));

  expect(mocks.deleteMutate).toHaveBeenCalled();
  expect(mocks.navigate).toHaveBeenCalledWith(
    expect.objectContaining({ to: "/cubes", ignoreBlocker: true }),
  );
});

test("delete does nothing when the confirm is declined", () => {
  vi.stubGlobal(
    "confirm",
    vi.fn(() => false),
  );

  render(<CubeSettingsSection cube={cube} />);
  fireEvent.click(screen.getByRole("button", { name: /delete cube/i }));

  expect(mocks.deleteMutate).not.toHaveBeenCalled();
  expect(mocks.navigate).not.toHaveBeenCalled();
});
