import { expect, test } from "vitest";
import { remainingLabel } from "./countdown";

const base = Date.parse("2026-07-13T12:00:00Z");

test("hours and minutes", () => {
  expect(remainingLabel("2026-07-14T11:30:00Z", base)).toBe("23h 30m");
});
test("minutes only", () => {
  expect(remainingLabel("2026-07-13T12:45:00Z", base)).toBe("45m");
});
test("past deadline clamps to zero", () => {
  expect(remainingLabel("2026-07-13T11:00:00Z", base)).toBe("0m");
});
