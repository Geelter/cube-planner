import { m } from "@/paraglide/messages";
import { Button } from "@/shared/ui/button";
import type { CubeCardEntry } from "../api";
import type { GroupKind } from "../lib/grouping";
import { groupCards } from "../lib/grouping";
import type { PendingAction } from "../lib/pendingDiff";

export function EditableCardList({
  cards,
  serverByOracle,
  groupKind,
  dispatch,
}: {
  /** Preview entries (server list with pending deltas applied) — display only. */
  cards: CubeCardEntry[];
  /** True server entries by oracleId — the reducer needs their untouched quantities. */
  serverByOracle: Map<string, CubeCardEntry>;
  groupKind: GroupKind;
  dispatch: (action: PendingAction) => void;
}) {
  if (cards.length === 0) {
    return <p className="text-sm text-fg-muted">{m.cubes_empty_list()}</p>;
  }
  const groups = groupCards(cards, groupKind);
  // Pending-only adds have no server entry; for those the preview entry is
  // correct (the reducer cancels against the pending add, never the cap).
  const serverEntry = (card: CubeCardEntry) => serverByOracle.get(card.oracleId) ?? card;
  return (
    <div className="columns-1 gap-6 sm:columns-2">
      {groups.map((group) => (
        <section key={group.key} className="mb-6 break-inside-avoid">
          <h3 className="mb-1 border-b border-border pb-1 text-sm font-semibold text-fg">
            {group.label} <span className="font-normal text-fg-muted">({group.cards.length})</span>
          </h3>
          <ul>
            {group.cards.map((card) => (
              <li
                key={card.oracleId}
                className="flex items-center justify-between gap-2 text-sm leading-7"
              >
                <span className="truncate text-fg">
                  {card.quantity > 1 && (
                    <span className="mr-1 font-semibold text-accent">{card.quantity}×</span>
                  )}
                  {card.name}
                </span>
                <span className="flex shrink-0 items-center gap-1">
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    aria-label={m.cubes_qty_decrease({ name: card.name })}
                    onClick={() => dispatch({ type: "decrement", entry: serverEntry(card) })}
                  >
                    −
                  </Button>
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    aria-label={m.cubes_qty_increase({ name: card.name })}
                    onClick={() => dispatch({ type: "increment", entry: serverEntry(card) })}
                  >
                    +
                  </Button>
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    aria-label={m.cubes_remove_card({ name: card.name })}
                    onClick={() => dispatch({ type: "remove", entry: serverEntry(card) })}
                  >
                    ✕
                  </Button>
                </span>
              </li>
            ))}
          </ul>
        </section>
      ))}
    </div>
  );
}
