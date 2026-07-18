import { m } from "@/paraglide/messages";
import type { CubeCardEntry } from "../api";

export type GroupKind = "color" | "type" | "cmc";

export type CardGroup = { key: string; label: string; cards: CubeCardEntry[] };

const COLOR_ORDER = ["W", "U", "B", "R", "G", "multicolor", "colorless", "land"] as const;

function colorLabels(): Record<string, string> {
  return {
    W: m.cubes_bucket_white(),
    U: m.cubes_bucket_blue(),
    B: m.cubes_bucket_black(),
    R: m.cubes_bucket_red(),
    G: m.cubes_bucket_green(),
    multicolor: m.cubes_bucket_multicolor(),
    colorless: m.cubes_bucket_colorless(),
    land: m.cubes_bucket_land(),
  };
}

// Pending adds are built from CardSummary (see CubeEditorPage.previewEntries),
// which the API doesn't return colors/colorIdentity for — the card hasn't
// been saved to the cube yet, so there's no CubeCardEntry row to read them
// from. manaCost IS available on CardSummary, so we derive color from its
// WUBRG pips as a fallback. This is a real derivation, not a guess: Scryfall
// mana costs use the same letters as the colors array, and cards with no
// colored pips (true colorless artifacts, lands) correctly parse to no
// colors either way. The one gap is color-indicator cards with an empty
// mana cost (e.g. Dryad Arbor) — vanishingly rare in cube contexts, and
// lands already bucket on type before color regardless.
function colorsFromManaCost(manaCost: string): string[] {
  return [...new Set(manaCost.match(/[WUBRG]/g) ?? [])];
}

function colorBucket(card: CubeCardEntry): string {
  // Type wins over color: dual lands with a color identity are lands.
  if (card.typeLine.includes("Land")) return "land";
  const colors =
    card.colors && card.colors.length > 0 ? card.colors : colorsFromManaCost(card.manaCost);
  if (colors.length === 0) return "colorless";
  if (colors.length > 1) return "multicolor";
  return colors[0] ?? "colorless";
}

// Primary card type for grouping: last supertype-stripped word before "—".
const TYPE_PRIORITY = [
  "Creature",
  "Planeswalker",
  "Battle",
  "Instant",
  "Sorcery",
  "Artifact",
  "Enchantment",
  "Land",
] as const;

function typeBucket(card: CubeCardEntry): string {
  const face = card.typeLine.split("//")[0] ?? card.typeLine;
  for (const t of TYPE_PRIORITY) {
    if (face.includes(t)) return t;
  }
  return face.split("—")[0]?.trim() ?? card.typeLine;
}

function cmcBucket(card: CubeCardEntry): string {
  const n = Math.floor(card.cmc);
  return n >= 7 ? "7+" : String(n);
}

function sortWithin(cards: CubeCardEntry[]): CubeCardEntry[] {
  return [...cards].sort((a, b) => a.cmc - b.cmc || a.name.localeCompare(b.name));
}

export function groupCards(cards: CubeCardEntry[], kind: GroupKind): CardGroup[] {
  const buckets = new Map<string, CubeCardEntry[]>();
  const bucketOf = kind === "color" ? colorBucket : kind === "type" ? typeBucket : cmcBucket;
  for (const card of cards) {
    const key = bucketOf(card);
    const list = buckets.get(key);
    if (list) list.push(card);
    else buckets.set(key, [card]);
  }

  let orderedKeys: string[];
  if (kind === "color") {
    orderedKeys = COLOR_ORDER.filter((k) => buckets.has(k));
  } else if (kind === "cmc") {
    orderedKeys = [...buckets.keys()].sort((a, b) => {
      if (a === "7+") return 1;
      if (b === "7+") return -1;
      return Number(a) - Number(b);
    });
  } else {
    orderedKeys = [...buckets.keys()].sort((a, b) => {
      const ia = TYPE_PRIORITY.indexOf(a as (typeof TYPE_PRIORITY)[number]);
      const ib = TYPE_PRIORITY.indexOf(b as (typeof TYPE_PRIORITY)[number]);
      if (ia !== -1 && ib !== -1) return ia - ib;
      if (ia !== -1) return -1;
      if (ib !== -1) return 1;
      return a.localeCompare(b);
    });
  }

  const labels = kind === "color" ? colorLabels() : null;
  return orderedKeys.map((key) => ({
    key,
    label: labels?.[key] ?? key,
    cards: sortWithin(buckets.get(key) ?? []),
  }));
}
