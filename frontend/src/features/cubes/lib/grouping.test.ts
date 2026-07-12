import { describe, expect, test } from "vitest";
import type { CubeCardEntry } from "../api";
import { groupCards } from "./grouping";

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

describe("groupCards by color", () => {
  test("buckets mono, multi, colorless, land; sorts CMC within", () => {
    const cards = [
      entry({ oracleId: "a", name: "Bolt", colors: ["R"], cmc: 1 }),
      entry({ oracleId: "b", name: "Fireball", colors: ["R"], cmc: 3 }),
      entry({ oracleId: "c", name: "Izzet Charm", colors: ["U", "R"], cmc: 2 }),
      entry({ oracleId: "d", name: "Sol Ring", typeLine: "Artifact", cmc: 1 }),
      entry({ oracleId: "e", name: "Island", typeLine: "Basic Land — Island", cmc: 0 }),
    ];
    const groups = groupCards(cards, "color");
    const byKey = new Map(groups.map((g) => [g.key, g]));
    expect(byKey.get("R")?.cards.map((c) => c.name)).toEqual(["Bolt", "Fireball"]);
    expect(byKey.get("multicolor")?.cards.map((c) => c.name)).toEqual(["Izzet Charm"]);
    expect(byKey.get("colorless")?.cards.map((c) => c.name)).toEqual(["Sol Ring"]);
    expect(byKey.get("land")?.cards.map((c) => c.name)).toEqual(["Island"]);
    // Lands bucket on type BEFORE color: a dual land with colors is still a land.
    // Empty buckets are omitted entirely.
    expect(groups.every((g) => g.cards.length > 0)).toBe(true);
    // Bucket order: W, U, B, R, G, multicolor, colorless, land.
    expect(groups.map((g) => g.key)).toEqual(["R", "multicolor", "colorless", "land"]);
  });

  test("cards of equal cmc sort by name", () => {
    const cards = [
      entry({ oracleId: "a", name: "Shock", colors: ["R"], cmc: 1 }),
      entry({ oracleId: "b", name: "Bolt", colors: ["R"], cmc: 1 }),
    ];
    const groups = groupCards(cards, "color");
    expect(groups[0]?.cards.map((c) => c.name)).toEqual(["Bolt", "Shock"]);
  });
});

describe("groupCards by type", () => {
  test("uses primary type, ignoring supertypes", () => {
    const cards = [
      entry({ oracleId: "a", name: "Bolt", typeLine: "Instant" }),
      entry({ oracleId: "b", name: "Goyf", typeLine: "Creature — Lhurgoyf" }),
      entry({ oracleId: "c", name: "Karn", typeLine: "Legendary Planeswalker — Karn" }),
    ];
    const keys = groupCards(cards, "type").map((g) => g.key);
    expect(keys).toContain("Creature");
    expect(keys).toContain("Instant");
    expect(keys).toContain("Planeswalker");
  });
});

describe("groupCards by cmc", () => {
  test("buckets 0..6 and 7+", () => {
    const cards = [
      entry({ oracleId: "a", name: "Zero", cmc: 0 }),
      entry({ oracleId: "b", name: "Big", cmc: 9 }),
    ];
    const keys = groupCards(cards, "cmc").map((g) => g.key);
    expect(keys).toEqual(["0", "7+"]);
  });
});
