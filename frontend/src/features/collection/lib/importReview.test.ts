import { expect, test } from "vitest";
import type { ImportResolveLine } from "../api";
import { buildImportItems, defaultChoices } from "./importReview";

const match = (scryfallId: string) => ({
  scryfallId,
  oracleId: "o",
  name: "Card",
  manaCost: "",
  typeLine: "",
  setCode: "tst",
  setName: "Test",
  collectorNumber: "1",
  imageSmall: null,
  imageNormal: null,
});

const lines: ImportResolveLine[] = [
  { lineNumber: 1, raw: "4 Bolt", quantity: 4, status: "matched", match: match("bolt") },
  {
    lineNumber: 2,
    raw: "Blot",
    quantity: 1,
    status: "ambiguous",
    suggestions: [match("s1"), match("s2")],
  },
  { lineNumber: 3, raw: "Gibberish", quantity: 1, status: "unmatched" },
];

test("defaultChoices: matched printing, top suggestion, skip for unmatched", () => {
  const choices = defaultChoices(lines);
  expect(choices.get(1)).toBe("bolt");
  expect(choices.get(2)).toBe("s1");
  expect(choices.get(3)).toBeNull();
});

test("buildImportItems drops skipped lines and keeps quantities", () => {
  const choices = defaultChoices(lines);
  expect(buildImportItems(lines, choices)).toEqual([
    { scryfallId: "bolt", quantity: 4 },
    { scryfallId: "s1", quantity: 1 },
  ]);
});

test("a manual skip removes an ambiguous line", () => {
  const choices = defaultChoices(lines);
  choices.set(2, null);
  expect(buildImportItems(lines, choices)).toEqual([{ scryfallId: "bolt", quantity: 4 }]);
});
