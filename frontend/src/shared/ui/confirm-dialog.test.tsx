import { render, screen, cleanup } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import { ConfirmDialog } from "./confirm-dialog";

afterEach(() => {
  cleanup();
});

test("renders the message and fires onConfirm", async () => {
  const onConfirm = vi.fn();
  render(
    <ConfirmDialog
      open
      onClose={() => {}}
      onConfirm={onConfirm}
      title="Remove Ann"
      message="Remove Ann from this event?"
      confirmLabel="Remove"
    />,
  );
  expect(screen.getByText("Remove Ann from this event?")).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Remove" }));
  expect(onConfirm).toHaveBeenCalledTimes(1);
});

test("cancel fires onClose but not onConfirm", async () => {
  const onClose = vi.fn();
  const onConfirm = vi.fn();
  render(
    <ConfirmDialog
      open
      onClose={onClose}
      onConfirm={onConfirm}
      title="T"
      message="M"
      confirmLabel="Go"
    />,
  );
  // Two buttons match /close/i here: the Dialog's own header "X" (aria-label
  // "Close") and this component's footer cancel button (text "Close"). The
  // footer one is rendered second.
  const closeButtons = screen.getAllByRole("button", { name: /close/i });
  await userEvent.click(closeButtons[1]!);
  expect(onClose).toHaveBeenCalled();
  expect(onConfirm).not.toHaveBeenCalled();
});

test("pending marks the confirm button busy", () => {
  render(
    <ConfirmDialog
      open
      onClose={() => {}}
      onConfirm={() => {}}
      title="T"
      message="M"
      confirmLabel="Go"
      pending
    />,
  );
  // The spinner's aria-label joins the accessible name, so match loosely
  // (see button.test.tsx).
  expect(screen.getByRole("button", { name: /Go/ })).toHaveAttribute("aria-busy", "true");
});
