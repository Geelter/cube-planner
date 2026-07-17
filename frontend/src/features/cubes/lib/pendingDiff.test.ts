import { describe, expect, test } from "vitest";
import type { CardSummary } from "@/shared/cards/api";
import type { CubeCardEntry } from "../api";
import { emptyPending, pendingCount, pendingReducer, toCommitDiff } from "./pendingDiff";

const bolt: CardSummary = {
  scryfallId: "s-bolt",
  oracleId: "o-bolt",
  name: "Lightning Bolt",
  manaCost: "{R}",
  typeLine: "Instant",
  imageSmall: null,
};

const boltEntry: CubeCardEntry = {
  scryfallId: "s-bolt",
  oracleId: "o-bolt",
  name: "Lightning Bolt",
  manaCost: "{R}",
  typeLine: "Instant",
  cmc: 1,
  colors: ["R"],
  colorIdentity: ["R"],
  rarity: "common",
  imageSmall: null,
  imageNormal: null,
  quantity: 2,
};

describe("pendingReducer", () => {
  test("add accumulates quantity", () => {
    let s = pendingReducer(emptyPending, { type: "add", card: bolt });
    s = pendingReducer(s, { type: "add", card: bolt });
    expect(toCommitDiff(s)).toEqual({
      adds: [{ scryfallId: "s-bolt", quantity: 2 }],
      removes: [],
    });
  });

  test("add then remove of same oracle cancels to no-op", () => {
    let s = pendingReducer(emptyPending, { type: "add", card: bolt });
    s = pendingReducer(s, { type: "decrement", entry: boltEntry });
    expect(pendingCount(s)).toBe(0);
    expect(toCommitDiff(s)).toEqual({ adds: [], removes: [] });
  });

  test("remove then add cancels symmetrically", () => {
    let s = pendingReducer(emptyPending, { type: "decrement", entry: boltEntry });
    s = pendingReducer(s, { type: "add", card: bolt });
    expect(toCommitDiff(s)).toEqual({ adds: [], removes: [] });
  });

  test("remove caps at current quantity", () => {
    let s = emptyPending;
    for (let i = 0; i < 5; i++) s = pendingReducer(s, { type: "decrement", entry: boltEntry });
    expect(toCommitDiff(s).removes).toEqual([{ oracleId: "o-bolt", quantity: 2 }]);
  });

  test("remove action removes the full current quantity at once", () => {
    const s = pendingReducer(emptyPending, { type: "remove", entry: boltEntry });
    expect(toCommitDiff(s).removes).toEqual([{ oracleId: "o-bolt", quantity: 2 }]);
  });

  test("undo clears one side only", () => {
    let s = pendingReducer(emptyPending, { type: "add", card: bolt });
    s = pendingReducer(s, { type: "undoAdd", oracleId: "o-bolt" });
    expect(pendingCount(s)).toBe(0);
  });

  test("revalidate clamps removes to fresh quantities and drops vanished cards", () => {
    let s = pendingReducer(emptyPending, { type: "remove", entry: boltEntry }); // remove 2
    const freshBolt = { ...boltEntry, quantity: 1 };
    s = pendingReducer(s, { type: "revalidate", current: [freshBolt] });
    expect(toCommitDiff(s).removes).toEqual([{ oracleId: "o-bolt", quantity: 1 }]);

    s = pendingReducer(s, { type: "revalidate", current: [] });
    expect(toCommitDiff(s).removes).toEqual([]);
  });

  test("reducer never mutates prior state", () => {
    const s1 = pendingReducer(emptyPending, { type: "add", card: bolt });
    pendingReducer(s1, { type: "add", card: bolt });
    expect(s1.adds.get("o-bolt")?.quantity).toBe(1);
  });
});

describe("remove clears the whole row", () => {
  test("remove with pending adds drops the adds and removes all server copies", () => {
    // 3 pending adds on top of 2 server copies; ✕ means "no Bolt at all".
    let s = pendingReducer(emptyPending, { type: "add", card: bolt });
    s = pendingReducer(s, { type: "add", card: bolt });
    s = pendingReducer(s, { type: "add", card: bolt });
    s = pendingReducer(s, { type: "remove", entry: boltEntry });
    expect(toCommitDiff(s)).toEqual({
      adds: [],
      removes: [{ oracleId: "o-bolt", quantity: 2 }],
    });
  });

  test("remove on a pending-only row (no server copies) just drops the adds", () => {
    let s = pendingReducer(emptyPending, { type: "add", card: bolt });
    s = pendingReducer(s, { type: "remove", entry: { ...boltEntry, quantity: 0 } });
    expect(pendingCount(s)).toBe(0);
    expect(toCommitDiff(s)).toEqual({ adds: [], removes: [] });
  });
});
