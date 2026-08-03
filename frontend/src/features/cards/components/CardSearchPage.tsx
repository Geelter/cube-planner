import { useState } from "react";
import { m } from "@/paraglide/messages";
import { Alert } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Label } from "@/shared/ui/label";
import { CardAutocomplete } from "@/shared/cards/CardAutocomplete";
import { ManaCost } from "@/shared/cards/ManaCost";
import { type CardSummary, useCardPrintings } from "@/shared/cards/api";
import { PrintingPickerDialog } from "@/shared/cards/PrintingPickerDialog";

function SelectedCardPanel({ card }: { card: CardSummary }) {
  const printings = useCardPrintings(card.oracleId);
  const [pickedId, setPickedId] = useState<string | null>(null);
  const [pickerOpen, setPickerOpen] = useState(false);
  const printingList = printings.data ?? [];
  const shown = printingList.find((p) => p.scryfallId === pickedId) ?? printingList[0];

  if (printings.isPending) {
    return <p className="text-sm text-fg-muted">{m.loading()}</p>;
  }
  if (printings.isError || shown === undefined) {
    return <Alert variant="danger">{m.error_generic()}</Alert>;
  }
  return (
    <div className="flex flex-wrap gap-6">
      {shown.imageNormal != null && (
        <img src={shown.imageNormal} alt={shown.name} className="w-64 self-start rounded-xl" />
      )}
      <div className="flex max-w-md flex-col gap-2">
        <h2 className="text-lg font-semibold text-fg">{shown.name}</h2>
        <p className="text-sm text-fg-muted">
          {shown.typeLine}
          {shown.manaCost !== "" && (
            <>
              {" · "}
              <ManaCost cost={shown.manaCost} />
            </>
          )}
        </p>
        {shown.oracleText !== "" && (
          <p className="text-sm whitespace-pre-line text-fg">{shown.oracleText}</p>
        )}
        <p className="text-sm text-fg-muted">
          {m.cards_set_line({ setName: shown.setName, collectorNumber: shown.collectorNumber })}
        </p>
        <p className="text-sm text-fg-muted">
          {m.cards_printings_count({ count: printings.data?.length ?? 0 })}
        </p>
        <div>
          <Button type="button" variant="outline" size="sm" onClick={() => setPickerOpen(true)}>
            {m.cards_change_printing()}
          </Button>
        </div>
      </div>
      <PrintingPickerDialog
        open={pickerOpen}
        onClose={() => setPickerOpen(false)}
        oracleId={card.oracleId}
        name={shown.name}
        currentScryfallId={shown.scryfallId}
        onPick={(id) => {
          setPickedId(id);
          setPickerOpen(false);
        }}
      />
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
