// Cardmarket's Wants import accepts plain "<amount> <card name>" lines.
export function wantlistToCardmarketText(
  items: readonly { missingQuantity: number; name: string }[],
): string {
  return items.map((i) => `${i.missingQuantity} ${i.name}`).join("\n");
}

// Moxfield/Archidekt accept a trailing "(SET)" plus collector number; this
// row is a specific printing (that's the whole point of exact-printing
// mode), so the collector number goes along with the set code — that's
// also what makes this round-trip back into our own importer, which reads
// this exact "<qty> <name> (<SET>) <number>" grammar. Cardmarket does not
// accept any of this, which is why this is a separate export rather than a
// change to the one above.
export function wantlistToSetAnnotatedText(
  items: readonly {
    missingQuantity: number;
    name: string;
    setCode: string;
    collectorNumber: string;
  }[],
): string {
  return items
    .map((i) => `${i.missingQuantity} ${i.name} (${i.setCode.toUpperCase()}) ${i.collectorNumber}`)
    .join("\n");
}

export function wantlistFilename(cubeName: string): string {
  const slug =
    cubeName
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "") || "cube";
  return `${slug}-wantlist.txt`;
}
