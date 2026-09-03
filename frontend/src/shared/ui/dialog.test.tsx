import { render, screen, cleanup } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import { Dialog } from "./dialog";

afterEach(() => {
  cleanup();
});

test("renders title and children when open, nothing when closed", () => {
  const { rerender } = render(
    <Dialog open={false} onClose={() => {}} title="Pick a card">
      <p>Body text</p>
    </Dialog>,
  );
  expect(screen.queryByText("Body text")).not.toBeInTheDocument();
  rerender(
    <Dialog open onClose={() => {}} title="Pick a card">
      <p>Body text</p>
    </Dialog>,
  );
  expect(screen.getByRole("heading", { name: "Pick a card" })).toBeInTheDocument();
  expect(screen.getByText("Body text")).toBeInTheDocument();
});

test("close button fires onClose", async () => {
  const onClose = vi.fn();
  render(
    <Dialog open onClose={onClose} title="Pick a card">
      <p>Body</p>
    </Dialog>,
  );
  await userEvent.click(screen.getByRole("button", { name: "Close" }));
  expect(onClose).toHaveBeenCalled();
});

test("locks body scroll while open and releases when closed", () => {
  const { rerender } = render(
    <Dialog open={false} onClose={() => {}} title="T">
      <p>Body</p>
    </Dialog>,
  );
  expect(document.body.style.overflow).toBe("");
  rerender(
    <Dialog open onClose={() => {}} title="T">
      <p>Body</p>
    </Dialog>,
  );
  expect(document.body.style.overflow).toBe("hidden");
  rerender(
    <Dialog open={false} onClose={() => {}} title="T">
      <p>Body</p>
    </Dialog>,
  );
  expect(document.body.style.overflow).toBe("");
});

test("closes the dialog element when unmounted while still open", () => {
  const { unmount } = render(
    <Dialog open onClose={() => {}} title="T">
      <p>Body</p>
    </Dialog>,
  );
  const el = screen.getByRole("dialog") as HTMLDialogElement;
  const close = vi.spyOn(el, "close");
  unmount();
  expect(close).toHaveBeenCalled();
  expect(document.body.style.overflow).toBe("");
});
