import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { LanguageSwitcher } from "./LanguageSwitcher";

const setLocale = vi.fn();
vi.mock("@/paraglide/runtime", () => ({
  locales: ["en", "pl"],
  getLocale: () => "en",
  setLocale: (l: string) => setLocale(l),
  // The generated messages module reads this directly; keep it defined so
  // m.lang_label() resolves via getLocale() like it does at runtime.
  experimentalStaticLocale: undefined,
}));

describe("LanguageSwitcher", () => {
  it("marks the active locale and switches on click", async () => {
    render(<LanguageSwitcher />);
    const en = screen.getByRole("button", { name: "EN" });
    const pl = screen.getByRole("button", { name: "PL" });
    expect(en).toHaveAttribute("aria-pressed", "true");
    expect(pl).toHaveAttribute("aria-pressed", "false");
    await userEvent.click(pl);
    expect(setLocale).toHaveBeenCalledWith("pl");
  });
});
