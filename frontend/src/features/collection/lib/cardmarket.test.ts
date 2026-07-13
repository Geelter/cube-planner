import { expect, test } from "vitest";
import { wantlistFilename, wantlistToCardmarketText } from "./cardmarket";

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
