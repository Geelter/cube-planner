import { Monitor, Moon, Sun } from "lucide-react";
import { useState } from "react";
import { m } from "@/paraglide/messages";
import { getThemeSetting, setThemeSetting, type ThemeSetting } from "@/shared/lib/theme";
import { Button } from "@/shared/ui/button";

const NEXT: Record<ThemeSetting, ThemeSetting> = {
  system: "light",
  light: "dark",
  dark: "system",
};

const LABEL: Record<ThemeSetting, () => string> = {
  light: m.theme_light,
  dark: m.theme_dark,
  system: m.theme_system,
};

export function ThemeToggle() {
  const [setting, setSetting] = useState<ThemeSetting>(getThemeSetting);
  const Icon = setting === "light" ? Sun : setting === "dark" ? Moon : Monitor;
  return (
    <Button
      variant="ghost"
      size="icon"
      aria-label={LABEL[setting]()}
      title={LABEL[setting]()}
      onClick={() => {
        const next = NEXT[setting];
        setThemeSetting(next);
        setSetting(next);
      }}
    >
      <Icon aria-hidden className="size-4" />
    </Button>
  );
}
