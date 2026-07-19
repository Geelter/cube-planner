import { useState } from "react";
import { m } from "@/paraglide/messages";
import { useDebouncedValue } from "@/shared/lib/useDebouncedValue";
import { Combobox } from "@/shared/ui/combobox";
import { useCardAutocomplete, type CardSummary } from "./api";
import { ManaCost } from "./ManaCost";

type CardAutocompleteProps = {
  id: string;
  onSelect: (card: CardSummary) => void;
};

export function CardAutocomplete({ id, onSelect }: CardAutocompleteProps) {
  const [query, setQuery] = useState("");
  const debounced = useDebouncedValue(query, 250);
  const results = useCardAutocomplete(debounced);

  return (
    <Combobox
      id={id}
      value={query}
      onValueChange={setQuery}
      options={results.data ?? []}
      getOptionId={(c) => c.scryfallId}
      renderOption={(c) => (
        <span className="flex items-center gap-3">
          {c.imageSmall != null && (
            <img src={c.imageSmall} alt="" loading="lazy" className="h-12 w-auto rounded-sm" />
          )}
          <span className="flex flex-col">
            <span className="font-medium">{c.name}</span>
            <span className="text-xs text-fg-muted">
              {c.typeLine}
              {c.manaCost !== "" && (
                <>
                  {" · "}
                  <ManaCost cost={c.manaCost} />
                </>
              )}
            </span>
          </span>
        </span>
      )}
      onSelect={(c) => {
        onSelect(c);
        setQuery(c.name);
      }}
      loading={results.isFetching}
      minChars={2}
      emptyMessage={m.cards_no_results()}
      loadingMessage={m.loading()}
      placeholder={m.cards_search_placeholder()}
    />
  );
}
