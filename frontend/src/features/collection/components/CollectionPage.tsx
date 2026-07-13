import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { m } from "@/paraglide/messages";
import { CardAutocomplete } from "@/shared/cards/CardAutocomplete";
import { CardHoverPreview } from "@/shared/cards/CardHoverPreview";
import { useDebouncedValue } from "@/shared/lib/useDebouncedValue";
import { Alert } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Input } from "@/shared/ui/input";
import { Label } from "@/shared/ui/label";
import {
  COLLECTION_PAGE_SIZE,
  UnauthorizedError,
  useCollection,
  useImportItems,
  useSetQuantity,
} from "../api";
import { QuantityStepper } from "./QuantityStepper";

export function CollectionPage() {
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(0);
  const debouncedSearch = useDebouncedValue(search, 300);
  const collection = useCollection(debouncedSearch, page);
  const setQuantity = useSetQuantity();
  const importItems = useImportItems();

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
                  aria-label={m.collection_remove_card({ name: item.name })}
                  onClick={() => setQuantity.mutate({ scryfallId: item.scryfallId, quantity: 0 })}
                >
                  ✕
                </Button>
              </span>
            </li>
          ))}
        </ul>
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
