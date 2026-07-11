import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { getThemeSetting, initTheme, setThemeSetting } from "./theme";

type Listener = (e: { matches: boolean }) => void;

function stubMatchMedia(matches: boolean) {
  const listeners: Listener[] = [];
  const mql = {
    matches,
    addEventListener: (_: string, cb: Listener) => listeners.push(cb),
    removeEventListener: () => undefined,
  };
  vi.stubGlobal("matchMedia", () => mql);
  return {
    fireChange(next: boolean) {
      mql.matches = next;
      for (const cb of listeners) cb({ matches: next });
    },
  };
}

describe("theme", () => {
  beforeEach(() => localStorage.clear());
  afterEach(() => vi.unstubAllGlobals());

  it("defaults to system", () => {
    stubMatchMedia(false);
    expect(getThemeSetting()).toBe("system");
  });

  it("applies and persists an explicit setting", () => {
    stubMatchMedia(false);
    setThemeSetting("dark");
    expect(localStorage.getItem("theme")).toBe("dark");
    expect(document.documentElement.dataset["theme"]).toBe("dark");
    setThemeSetting("light");
    expect(document.documentElement.dataset["theme"]).toBe("light");
  });

  it("system resolves via prefers-color-scheme", () => {
    stubMatchMedia(true);
    setThemeSetting("system");
    expect(document.documentElement.dataset["theme"]).toBe("dark");
  });

  it("reacts to OS changes while in system mode", () => {
    const media = stubMatchMedia(false);
    initTheme();
    expect(document.documentElement.dataset["theme"]).toBe("light");
    media.fireChange(true);
    expect(document.documentElement.dataset["theme"]).toBe("dark");
  });

  it("ignores OS changes when set explicitly", () => {
    const media = stubMatchMedia(false);
    initTheme();
    setThemeSetting("light");
    media.fireChange(true);
    expect(document.documentElement.dataset["theme"]).toBe("light");
  });
});
