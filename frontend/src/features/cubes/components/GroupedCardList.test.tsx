import { render, screen } from "@testing-library/react";
import { expect, test } from "vitest";
import type { CubeCardEntry } from "../api";
import { GroupedCardList } from "./GroupedCardList";

function entry(over: Partial<CubeCardEntry>): CubeCardEntry {
  return {
    scryfallId: "s",
    oracleId: "o",
    name: "Card",
    manaCost: "",
    typeLine: "Instant",
    cmc: 1,
    colors: [],
    colorIdentity: [],
    rarity: "common",
    imageSmall: null,
    imageNormal: null,
    quantity: 1,
    ...over,
  };
}

test("renders color group headings and quantity badges", () => {
  render(
    <GroupedCardList
      groupKind="color"
      cards={[
        entry({
          oracleId: "a",
          name: "Lightning Bolt",
          colors: ["R"],
          manaCost: "{R}",
          quantity: 2,
        }),
        entry({ oracleId: "b", name: "Island", typeLine: "Basic Land — Island" }),
      ]}
    />,
  );
  expect(screen.getByRole("heading", { name: /red/i })).toBeDefined();
  expect(screen.getByRole("heading", { name: /lands/i })).toBeDefined();
  expect(screen.getByText("2×")).toBeDefined();
  expect(screen.getByText("Lightning Bolt")).toBeDefined();
  expect(screen.getByRole("img", { name: "{R}" })).toBeInTheDocument();
});

test("renders empty state", () => {
  render(<GroupedCardList groupKind="color" cards={[]} />);
  expect(screen.getByText(/no cards/i)).toBeDefined();
});
