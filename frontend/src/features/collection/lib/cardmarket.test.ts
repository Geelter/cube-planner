import { expect, test } from "vitest";
import {
  wantlistFilename,
  wantlistToCardmarketText,
  wantlistToSetAnnotatedText,
} from "./cardmarket";

test("one '<qty> <name>' line per entry", () => {
  expect(
    wantlistToCardmarketText([
      { missingQuantity: 1, name: "Lightning Bolt" },
      { missingQuantity: 3, name: "Borrowing 100,000 Arrows" },
    ]),
  ).toBe("1 Lightning Bolt\n3 Borrowing 100,000 Arrows");
});

test("empty list gives an empty string", () => {
  expect(wantlistToCardmarketText([])).toBe("");
});

test("filename slugs the cube name", () => {
  expect(wantlistFilename("Mat's Vintage Cube!")).toBe("mat-s-vintage-cube-wantlist.txt");
  expect(wantlistFilename("***")).toBe("cube-wantlist.txt");
});

const setItems = [
  { missingQuantity: 1, name: "Lightning Bolt", setCode: "leb", collectorNumber: "162" },
  { missingQuantity: 2, name: "Brainstorm", setCode: "mmq", collectorNumber: "45" },
];

test("the Cardmarket export stays quantity + name only", () => {
  expect(wantlistToCardmarketText(setItems)).toBe("1 Lightning Bolt\n2 Brainstorm");
});

test("the set-annotated export appends an uppercased set code and the collector number, so it round-trips into the importer's printing-selector grammar", () => {
  expect(wantlistToSetAnnotatedText(setItems)).toBe(
    "1 Lightning Bolt (LEB) 162\n2 Brainstorm (MMQ) 45",
  );
});

test("set-annotated export of an empty list gives an empty string", () => {
  expect(wantlistToSetAnnotatedText([])).toBe("");
});
