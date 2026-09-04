// Cardmarket's Wants import accepts plain "<amount> <card name>" lines.
export function wantlistToCardmarketText(
  items: readonly { missingQuantity: number; name: string }[],
): string {
  return items.map((i) => `${i.missingQuantity} ${i.name}`).join("\n");
}

// Moxfield/Archidekt accept a trailing "(SET)"; Cardmarket does not, which
// is why this is a separate export rather than a change to the one above.
export function wantlistToSetAnnotatedText(
  items: readonly { missingQuantity: number; name: string; setCode: string }[],
): string {
  return items.map((i) => `${i.missingQuantity} ${i.name} (${i.setCode.toUpperCase()})`).join("\n");
}

export function wantlistFilename(cubeName: string): string {
  const slug =
    cubeName
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "") || "cube";
  return `${slug}-wantlist.txt`;
}
