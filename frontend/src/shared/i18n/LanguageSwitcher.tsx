import { m } from "@/paraglide/messages";
import { getLocale, locales, setLocale } from "@/paraglide/runtime";
import { Button } from "@/shared/ui/button";

export function LanguageSwitcher() {
  const current = getLocale();
  return (
    <fieldset aria-label={m.lang_label()} className="m-0 flex gap-1 border-0 p-0">
      {locales.map((locale) => (
        <Button
          key={locale}
          variant={locale === current ? "outline" : "ghost"}
          size="sm"
          aria-pressed={locale === current}
          onClick={() => setLocale(locale)}
        >
          {locale.toUpperCase()}
        </Button>
      ))}
    </fieldset>
  );
}
