import { useState } from "react";
import { m } from "@/paraglide/messages";
import { Alert } from "@/shared/ui/alert";
import { Label } from "@/shared/ui/label";
import { useCardPrintings, type CardSummary } from "../api";
import { CardAutocomplete } from "./CardAutocomplete";

function SelectedCardPanel({ card }: { card: CardSummary }) {
  const printings = useCardPrintings(card.oracleId);
  const latest = printings.data?.[0];

  if (printings.isPending) {
    return <p className="text-sm text-fg-muted">{m.loading()}</p>;
  }
  if (printings.isError || latest === undefined) {
    return <Alert variant="danger">{m.error_generic()}</Alert>;
  }
  return (
    <div className="flex flex-wrap gap-6">
      {latest.imageNormal != null && (
        <img src={latest.imageNormal} alt={latest.name} className="w-64 self-start rounded-xl" />
      )}
      <div className="flex max-w-md flex-col gap-2">
        <h2 className="text-lg font-semibold text-fg">{latest.name}</h2>
        <p className="text-sm text-fg-muted">
          {latest.typeLine}
          {latest.manaCost !== "" && ` · ${latest.manaCost}`}
        </p>
        {latest.oracleText !== "" && (
          <p className="text-sm whitespace-pre-line text-fg">{latest.oracleText}</p>
        )}
        <p className="text-sm text-fg-muted">
          {m.cards_set_line({ setName: latest.setName, collectorNumber: latest.collectorNumber })}
        </p>
        <p className="text-sm text-fg-muted">
          {m.cards_printings_count({ count: printings.data?.length ?? 0 })}
        </p>
      </div>
    </div>
  );
}

export function CardSearchPage() {
  const [selected, setSelected] = useState<CardSummary | null>(null);

  return (
    <div className="flex flex-col gap-6">
      <h1 className="text-2xl font-semibold text-fg">{m.cards_title()}</h1>
      <div className="flex max-w-md flex-col gap-1.5">
        <Label htmlFor="card-search">{m.cards_search_label()}</Label>
        <CardAutocomplete id="card-search" onSelect={setSelected} />
      </div>
      {selected === null ? (
        <p className="text-sm text-fg-muted">{m.cards_select_hint()}</p>
      ) : (
        <SelectedCardPanel key={selected.scryfallId} card={selected} />
      )}
    </div>
  );
}
