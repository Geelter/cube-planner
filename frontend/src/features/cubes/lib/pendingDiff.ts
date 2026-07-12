import type { CardSummary } from "@/shared/cards/api";
import type { CubeCardEntry } from "../api";

// Net-delta pending diff, keyed by oracleId. An add and a remove of the
// same oracle cancel against each other, so the committed diff never
// contains the same card on both sides (the API rejects that).
export type PendingState = {
  adds: Map<string, { card: CardSummary; quantity: number }>;
  removes: Map<string, { entry: CubeCardEntry; quantity: number }>;
};

export type PendingAction =
  | { type: "add"; card: CardSummary }
  | { type: "increment"; entry: CubeCardEntry }
  | { type: "decrement"; entry: CubeCardEntry }
  | { type: "remove"; entry: CubeCardEntry }
  | { type: "undoAdd"; oracleId: string }
  | { type: "undoRemove"; oracleId: string }
  | { type: "revalidate"; current: CubeCardEntry[] }
  | { type: "reset" };

export const emptyPending: PendingState = { adds: new Map(), removes: new Map() };

function clone(state: PendingState): PendingState {
  return { adds: new Map(state.adds), removes: new Map(state.removes) };
}

function summaryFromEntry(entry: CubeCardEntry): CardSummary {
  return {
    scryfallId: entry.scryfallId,
    oracleId: entry.oracleId,
    name: entry.name,
    manaCost: entry.manaCost,
    typeLine: entry.typeLine,
    imageSmall: entry.imageSmall,
  };
}

function bumpAdd(state: PendingState, card: CardSummary, by: number): PendingState {
  const next = clone(state);
  const pendingRemove = next.removes.get(card.oracleId);
  if (pendingRemove) {
    // Cancel against the remove side first.
    if (pendingRemove.quantity > by) {
      next.removes.set(card.oracleId, { ...pendingRemove, quantity: pendingRemove.quantity - by });
      return next;
    }
    next.removes.delete(card.oracleId);
    const rest = by - pendingRemove.quantity;
    if (rest === 0) return next;
    by = rest;
  }
  const existing = next.adds.get(card.oracleId);
  next.adds.set(card.oracleId, { card, quantity: (existing?.quantity ?? 0) + by });
  return next;
}

function bumpRemove(state: PendingState, entry: CubeCardEntry, by: number): PendingState {
  const next = clone(state);
  const pendingAdd = next.adds.get(entry.oracleId);
  if (pendingAdd) {
    if (pendingAdd.quantity > by) {
      next.adds.set(entry.oracleId, { ...pendingAdd, quantity: pendingAdd.quantity - by });
      return next;
    }
    next.adds.delete(entry.oracleId);
    const rest = by - pendingAdd.quantity;
    if (rest === 0) return next;
    by = rest;
  }
  const existing = next.removes.get(entry.oracleId);
  const quantity = Math.min((existing?.quantity ?? 0) + by, entry.quantity);
  if (quantity <= 0) return next;
  next.removes.set(entry.oracleId, { entry, quantity });
  return next;
}

export function pendingReducer(state: PendingState, action: PendingAction): PendingState {
  switch (action.type) {
    case "add":
      return bumpAdd(state, action.card, 1);
    case "increment":
      return bumpAdd(state, summaryFromEntry(action.entry), 1);
    case "decrement":
      return bumpRemove(state, action.entry, 1);
    case "remove":
      return bumpRemove(state, action.entry, action.entry.quantity);
    case "undoAdd": {
      const next = clone(state);
      next.adds.delete(action.oracleId);
      return next;
    }
    case "undoRemove": {
      const next = clone(state);
      next.removes.delete(action.oracleId);
      return next;
    }
    case "revalidate": {
      const next = clone(state);
      const currentByOracle = new Map(action.current.map((e) => [e.oracleId, e]));
      for (const [oracleId, pending] of next.removes) {
        const fresh = currentByOracle.get(oracleId);
        if (!fresh) {
          next.removes.delete(oracleId);
        } else if (pending.quantity > fresh.quantity) {
          next.removes.set(oracleId, { entry: fresh, quantity: fresh.quantity });
        } else {
          next.removes.set(oracleId, { ...pending, entry: fresh });
        }
      }
      return next;
    }
    case "reset":
      return emptyPending;
  }
}

export function toCommitDiff(state: PendingState): {
  adds: { scryfallId: string; quantity: number }[];
  removes: { oracleId: string; quantity: number }[];
} {
  return {
    adds: [...state.adds.values()].map((a) => ({
      scryfallId: a.card.scryfallId,
      quantity: a.quantity,
    })),
    removes: [...state.removes.entries()].map(([oracleId, r]) => ({
      oracleId,
      quantity: r.quantity,
    })),
  };
}

export function pendingCount(state: PendingState): number {
  return state.adds.size + state.removes.size;
}
