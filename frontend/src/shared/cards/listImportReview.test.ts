import { expect, test } from "vitest";
import type { ImportResolveLine } from "./useResolveCardList";
import { buildImportItems, defaultChoices } from "./listImportReview";

const match = (scryfallId: string) => ({
  scryfallId,
  oracleId: "o",
  name: "Card",
  manaCost: "",
  typeLine: "",
  setCode: "tst",
  setName: "Test",
  collectorNumber: "1",
  colors: [] as string[],
  imageSmall: null,
  imageNormal: null,
});

const lines: ImportResolveLine[] = [
  {
    lineNumber: 1,
    raw: "4 Bolt",
    quantity: 4,
    status: "matched",
    setCode: "",
    collectorNumber: "",
    match: match("bolt"),
  },
  {
    lineNumber: 2,
    raw: "Blot",
    quantity: 1,
    status: "ambiguous",
    setCode: "",
    collectorNumber: "",
    suggestions: [match("s1"), match("s2")],
  },
  {
    lineNumber: 3,
    raw: "Gibberish",
    quantity: 1,
    status: "unmatched",
    setCode: "",
    collectorNumber: "",
  },
];

const printingNotFoundLine: ImportResolveLine = {
  lineNumber: 4,
  raw: "1 Bolt (XYZ) 999",
  quantity: 1,
  status: "printing-not-found",
  setCode: "xyz",
  collectorNumber: "999",
  suggestions: [match("p1"), match("p2")],
};

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

test("defaultChoices pre-selects the top suggestion for printing-not-found, like ambiguous", () => {
  const choices = defaultChoices([printingNotFoundLine]);
  expect(choices.get(4)).toBe("p1");
});
