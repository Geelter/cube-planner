import { expect, test, vi } from "vitest";

vi.mock("@/paraglide/runtime", () => ({ getLocale: () => "en" }));

import { formatFee } from "./money";

test("formats cents as localized currency", () => {
  expect(formatFee(5000, "pln")).toMatch(/50/);
  expect(formatFee(5000, "pln")).toMatch(/PLN|zł/);
  expect(formatFee(150, "pln")).toMatch(/1[.,]50/);
});
