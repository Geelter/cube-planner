import type { ImportResolveLine } from "../api";

/** Chosen printing per lineNumber; null = skip the line. */
export type LineChoice = string | null;

export function defaultChoices(lines: ImportResolveLine[]): Map<number, LineChoice> {
  const choices = new Map<number, LineChoice>();
  for (const line of lines) {
    if (line.status === "matched" && line.match) {
      choices.set(line.lineNumber, line.match.scryfallId);
    } else if (line.status === "ambiguous") {
      choices.set(line.lineNumber, line.suggestions?.[0]?.scryfallId ?? null);
    } else {
      choices.set(line.lineNumber, null);
    }
  }
  return choices;
}

export function buildImportItems(
  lines: ImportResolveLine[],
  choices: Map<number, LineChoice>,
): { scryfallId: string; quantity: number }[] {
  const items: { scryfallId: string; quantity: number }[] = [];
  for (const line of lines) {
    const choice = choices.get(line.lineNumber);
    if (choice == null) continue; // the server merges duplicate printings
    items.push({ scryfallId: choice, quantity: line.quantity });
  }
  return items;
}
