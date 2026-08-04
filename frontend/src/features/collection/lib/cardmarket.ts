// Cardmarket's Wants import accepts plain "<amount> <card name>" lines.
export function wantlistToCardmarketText(
  items: readonly { missingQuantity: number; name: string }[],
): string {
  return items.map((i) => `${i.missingQuantity} ${i.name}`).join("\n");
}

export function wantlistFilename(cubeName: string): string {
  const slug =
    cubeName
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "") || "cube";
  return `${slug}-wantlist.txt`;
}
