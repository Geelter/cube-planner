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

// The className assertions pin the positioning contract of each side:
// right pins with ml-auto, bottom pins with mt-auto + full width.
test("default side stays the right-hand drawer", () => {
  render(
    <Drawer open onClose={() => {}} label="Menu">
      <p>Nav items</p>
    </Drawer>,
  );
  expect(screen.getByRole("dialog").className).toContain("ml-auto");
});

test("side=bottom pins to the viewport bottom and dismisses like the right drawer", async () => {
  const onClose = vi.fn();
  render(
    <Drawer open onClose={onClose} label="Pending changes" side="bottom">
      <p>Sheet content</p>
    </Drawer>,
  );
  const dialog = screen.getByRole("dialog", { name: "Pending changes" });
  expect(screen.getByText("Sheet content")).toBeInTheDocument();
  expect(dialog.className).toContain("mt-auto");
  expect(dialog.className).not.toContain("ml-auto");
  await userEvent.click(screen.getByRole("button", { name: "Close" }));
  expect(onClose).toHaveBeenCalledTimes(1);
  await userEvent.click(dialog); // dialog element itself = backdrop area
  expect(onClose).toHaveBeenCalledTimes(2);
});
