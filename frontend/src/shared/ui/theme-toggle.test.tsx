import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ThemeToggle } from "./theme-toggle";

describe("ThemeToggle", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.stubGlobal("matchMedia", () => ({
      matches: false,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
    }));
  });

  it("cycles system -> light -> dark -> system and persists", async () => {
    render(<ThemeToggle />);
    const btn = screen.getByRole("button");
    await userEvent.click(btn); // system -> light
    expect(localStorage.getItem("theme")).toBe("light");
    expect(document.documentElement.dataset["theme"]).toBe("light");
    await userEvent.click(btn); // light -> dark
    expect(localStorage.getItem("theme")).toBe("dark");
    expect(document.documentElement.dataset["theme"]).toBe("dark");
    await userEvent.click(btn); // dark -> system
    expect(localStorage.getItem("theme")).toBe("system");
  });
});
