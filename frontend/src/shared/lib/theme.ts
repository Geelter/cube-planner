export type ThemeSetting = "light" | "dark" | "system";

const STORAGE_KEY = "theme";
const DARK_QUERY = "(prefers-color-scheme: dark)";

export function getThemeSetting(): ThemeSetting {
  const v = localStorage.getItem(STORAGE_KEY);
  return v === "light" || v === "dark" ? v : "system";
}

function apply(setting: ThemeSetting): void {
  const resolved =
    setting === "system" ? (matchMedia(DARK_QUERY).matches ? "dark" : "light") : setting;
  document.documentElement.dataset["theme"] = resolved;
}

export function setThemeSetting(setting: ThemeSetting): void {
  localStorage.setItem(STORAGE_KEY, setting);
  apply(setting);
}

export function initTheme(): void {
  apply(getThemeSetting());
  matchMedia(DARK_QUERY).addEventListener("change", () => {
    if (getThemeSetting() === "system") apply("system");
  });
}
