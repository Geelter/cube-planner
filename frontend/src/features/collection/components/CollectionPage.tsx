import { Link } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { m } from "@/paraglide/messages";
import { CardAutocomplete } from "@/shared/cards/CardAutocomplete";
import { CardHoverPreview } from "@/shared/cards/CardHoverPreview";
import { PrintingPickerDialog } from "@/shared/cards/PrintingPickerDialog";
import { useDebouncedValue } from "@/shared/lib/useDebouncedValue";
import { Alert } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Input } from "@/shared/ui/input";
import { Label } from "@/shared/ui/label";
import {
  type CollectionItemEntry,
  COLLECTION_PAGE_SIZE,
  UnauthorizedError,
  useChangePrinting,
  useCollection,
  useImportItems,
  useSetQuantity,
} from "../api";
import { ImportDialog } from "./ImportDialog";
import { QuantityStepper } from "./QuantityStepper";

export function CollectionPage() {
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(0);
  const [pickerItem, setPickerItem] = useState<CollectionItemEntry | null>(null);
  const [importOpen, setImportOpen] = useState(false);
  const debouncedSearch = useDebouncedValue(search, 300);
  const collection = useCollection(debouncedSearch, page);
  const setQuantity = useSetQuantity();
  const importItems = useImportItems();
  const changePrinting = useChangePrinting();

  // A page beyond the end of the collection (e.g. the user just removed the
  // last item on it) comes back with an empty page and total=0 — clamp back
  // to page 0 instead of showing a false "collection is empty" state.
  useEffect(() => {
    if (!collection.isPending && page > 0 && collection.data?.items.length === 0) {
      setPage(0);
    }
  }, [collection.isPending, collection.data, page]);

  if (collection.isError && collection.error instanceof UnauthorizedError) {
    return (
      <Alert variant="default">
        {collection.error.message}{" "}
        <Link to="/login" className="font-medium underline">
          {m.nav_login()}
        </Link>
      </Alert>
    );
  }

  const pages = Math.max(1, Math.ceil((collection.data?.total ?? 0) / COLLECTION_PAGE_SIZE));
  const mutationError = setQuantity.error ?? changePrinting.error;
  const mutating = setQuantity.isPending || changePrinting.isPending;

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold text-fg">{m.collection_title()}</h1>
          {collection.data && (
            <p className="text-sm text-fg-muted">
              {m.collection_stats({
                cards: collection.data.total,
                copies: collection.data.totalCopies,
              })}
            </p>
          )}
        </div>
        <Button type="button" variant="outline" onClick={() => setImportOpen(true)}>
          {m.collection_import_button()}
        </Button>
      </div>

      <div className="grid gap-4 sm:grid-cols-2">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="collection-add">{m.collection_add_label()}</Label>
          <CardAutocomplete
            id="collection-add"
            onSelect={(card) =>
              importItems.mutate({ items: [{ scryfallId: card.scryfallId, quantity: 1 }] })
            }
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="collection-search">{m.collection_search_label()}</Label>
          <Input
            id="collection-search"
            type="search"
            maxLength={100}
            placeholder={m.collection_search_placeholder()}
            value={search}
            onChange={(e) => {
              setSearch(e.target.value);
              setPage(0);
            }}
          />
        </div>
      </div>

      {collection.isPending && <p className="text-sm text-fg-muted">{m.loading()}</p>}
      {collection.isError && !(collection.error instanceof UnauthorizedError) && (
        <Alert variant="danger">{collection.error.message}</Alert>
      )}
      {mutationError && <Alert variant="danger">{mutationError.message}</Alert>}
      {collection.data && collection.data.items.length === 0 && (
        <p className="text-sm text-fg-muted">
          {debouncedSearch === "" ? m.collection_empty() : m.collection_no_results()}
        </p>
      )}
      {collection.data && collection.data.items.length > 0 && (
        <ul className="divide-y divide-border">
          {collection.data.items.map((item) => (
            <li key={item.scryfallId} className="flex items-center justify-between gap-3 py-1.5">
              <CardHoverPreview card={item}>
                <span className="flex flex-col">
                  <span className="truncate text-sm text-fg">{item.name}</span>
                  <span className="text-xs text-fg-muted">
                    {m.cards_set_line({
                      setName: item.setName,
                      collectorNumber: item.collectorNumber,
                    })}
                  </span>
                </span>
              </CardHoverPreview>
              <span className="flex shrink-0 items-center gap-1">
                <QuantityStepper
                  name={item.name}
                  quantity={item.quantity}
                  onCommit={(quantity) =>
                    setQuantity.mutate({ scryfallId: item.scryfallId, quantity })
                  }
                />
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  aria-label={m.collection_change_printing({ name: item.name })}
                  disabled={mutating}
                  onClick={() => setPickerItem(item)}
                >
                  ⇄
                </Button>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  aria-label={m.collection_remove_card({ name: item.name })}
                  disabled={mutating}
                  onClick={() => setQuantity.mutate({ scryfallId: item.scryfallId, quantity: 0 })}
                >
                  ✕
                </Button>
              </span>
            </li>
          ))}
        </ul>
      )}

      <ImportDialog open={importOpen} onClose={() => setImportOpen(false)} />

      {pickerItem && (
        <PrintingPickerDialog
          open
          onClose={() => setPickerItem(null)}
          oracleId={pickerItem.oracleId}
          name={pickerItem.name}
          currentScryfallId={pickerItem.scryfallId}
          onPick={(newScryfallId) => {
            changePrinting.mutate({ scryfallId: pickerItem.scryfallId, newScryfallId });
            setPickerItem(null);
          }}
        />
      )}

      {collection.data && pages > 1 && (
        <div className="flex items-center gap-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={page === 0}
            onClick={() => setPage((p) => p - 1)}
            aria-label={m.pagination_prev()}
          >
            ←
          </Button>
          <span className="text-sm text-fg-muted">
            {page + 1} / {pages}
          </span>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={page + 1 >= pages}
            onClick={() => setPage((p) => p + 1)}
            aria-label={m.pagination_next()}
          >
            →
          </Button>
        </div>
      )}
    </div>
  );
}
