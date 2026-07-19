import { render, screen, cleanup } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import { Drawer } from "./drawer";

afterEach(() => {
  cleanup();
});

test("renders children when open, nothing when closed", () => {
  const { rerender } = render(
    <Drawer open={false} onClose={() => {}} label="Menu">
      <p>Nav items</p>
    </Drawer>,
  );
  expect(screen.queryByText("Nav items")).not.toBeInTheDocument();
  rerender(
    <Drawer open onClose={() => {}} label="Menu">
      <p>Nav items</p>
    </Drawer>,
  );
  expect(screen.getByText("Nav items")).toBeInTheDocument();
  expect(screen.getByRole("dialog")).toHaveAccessibleName("Menu");
});

test("close button fires onClose", async () => {
  const onClose = vi.fn();
  render(
    <Drawer open onClose={onClose} label="Menu">
      <p>Nav items</p>
    </Drawer>,
  );
  await userEvent.click(screen.getByRole("button", { name: "Close" }));
  expect(onClose).toHaveBeenCalled();
});

test("backdrop click fires onClose", async () => {
  const onClose = vi.fn();
  render(
    <Drawer open onClose={onClose} label="Menu">
      <p>Nav items</p>
    </Drawer>,
  );
  await userEvent.click(screen.getByRole("dialog", { name: "Menu" }));
  expect(onClose).toHaveBeenCalled();
});
