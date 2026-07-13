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

export function downloadTextFile(filename: string, text: string): void {
  const blob = new Blob([text], { type: "text/plain;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}
