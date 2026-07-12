import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { afterEach, expect, test, vi } from "vitest";
import { Combobox } from "./combobox";

afterEach(() => {
  cleanup();
});

type Fruit = { key: string; label: string };

const FRUITS: Fruit[] = [
  { key: "a", label: "Apple" },
  { key: "b", label: "Banana" },
  { key: "c", label: "Cherry" },
];

function Harness({ onSelect }: { onSelect: (f: Fruit) => void }) {
  const [value, setValue] = useState("");
  return (
    <Combobox
      id="fruit"
      value={value}
      onValueChange={setValue}
      options={value.length >= 2 ? FRUITS : []}
      getOptionId={(f) => f.key}
      renderOption={(f) => f.label}
      onSelect={onSelect}
      minChars={2}
      emptyMessage="No results."
      loadingMessage="Loading…"
    />
  );
}

test("opens on typing and selects with keyboard", async () => {
  const user = userEvent.setup();
  const onSelect = vi.fn();
  render(<Harness onSelect={onSelect} />);

  const input = screen.getByRole("combobox");
  expect(input).toHaveAttribute("aria-expanded", "false");

  await user.type(input, "ap");
  expect(input).toHaveAttribute("aria-expanded", "true");
  expect(screen.getAllByRole("option")).toHaveLength(3);

  await user.keyboard("{ArrowDown}{ArrowDown}{Enter}");
  expect(onSelect).toHaveBeenCalledWith(FRUITS[1]);
  // List closes after selection.
  expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
});

test("escape closes the list", async () => {
  const user = userEvent.setup();
  render(<Harness onSelect={vi.fn()} />);
  const input = screen.getByRole("combobox");
  await user.type(input, "ap");
  expect(screen.getByRole("listbox")).toBeInTheDocument();
  await user.keyboard("{Escape}");
  expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
});

test("click selects an option", async () => {
  const user = userEvent.setup();
  const onSelect = vi.fn();
  render(<Harness onSelect={onSelect} />);
  await user.type(screen.getByRole("combobox"), "ba");
  await user.click(screen.getByText("Cherry"));
  expect(onSelect).toHaveBeenCalledWith(FRUITS[2]);
});

test("stays closed below minChars and sets activedescendant", async () => {
  const user = userEvent.setup();
  render(<Harness onSelect={vi.fn()} />);
  const input = screen.getByRole("combobox");
  await user.type(input, "a");
  expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
  await user.type(input, "p");
  await user.keyboard("{ArrowDown}");
  const active = input.getAttribute("aria-activedescendant");
  expect(active).toBeTruthy();
  expect(document.getElementById(active as string)).toHaveTextContent("Apple");
});
