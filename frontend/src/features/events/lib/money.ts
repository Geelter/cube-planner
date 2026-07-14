import { getLocale } from "@/paraglide/runtime";

/** feeCents → localized currency string ("50,00 zł" / "PLN 50.00"). */
export function formatFee(feeCents: number, currency: string): string {
  return new Intl.NumberFormat(getLocale(), {
    style: "currency",
    currency: currency.toUpperCase(),
  }).format(feeCents / 100);
}
