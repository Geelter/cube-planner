import { m } from "@/paraglide/messages";
import type { CubeCardEntry } from "../api";
import type { GroupKind } from "../lib/grouping";
import { groupCards } from "../lib/grouping";
import { CardHoverPreview } from "@/shared/cards/CardHoverPreview";
import { ManaCost } from "@/shared/cards/ManaCost";

export function GroupedCardList({
  cards,
  groupKind,
}: {
  cards: CubeCardEntry[];
  groupKind: GroupKind;
}) {
  if (cards.length === 0) {
    return <p className="text-sm text-fg-muted">{m.cubes_empty_list()}</p>;
  }
  const groups = groupCards(cards, groupKind);
  return (
    <div className="columns-1 gap-6 sm:columns-2 lg:columns-3">
      {groups.map((group) => (
        <section key={group.key} className="mb-6 break-inside-avoid">
          <h3 className="mb-1 border-b border-border pb-1 text-sm font-semibold text-fg">
            {group.label} <span className="font-normal text-fg-muted">({group.cards.length})</span>
          </h3>
          <ul>
            {group.cards.map((card) => (
              <li key={card.oracleId} className="text-sm leading-6">
                <CardHoverPreview card={card}>
                  <button
                    type="button"
                    className="flex w-full justify-between gap-2 rounded px-1 hover:bg-surface-raised focus:bg-surface-raised focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
                  >
                    <span className="truncate text-fg">
                      {card.quantity > 1 && (
                        <span className="mr-1 font-semibold text-accent">{card.quantity}×</span>
                      )}
                      {card.name}
                    </span>
                    <span className="shrink-0">
                      <ManaCost cost={card.manaCost} />
                    </span>
                  </button>
                </CardHoverPreview>
              </li>
            ))}
          </ul>
        </section>
      ))}
    </div>
  );
}
