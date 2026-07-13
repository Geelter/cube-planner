import { useState } from "react";
import { m } from "@/paraglide/messages";
import { Alert } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Dialog } from "@/shared/ui/dialog";
import { Label } from "@/shared/ui/label";
import type { ImportResolveLine } from "../api";
import { useImportItems, useResolveImport } from "../api";
import type { LineChoice } from "../lib/importReview";
import { buildImportItems, defaultChoices } from "../lib/importReview";

export function ImportDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [text, setText] = useState("");
  const [lines, setLines] = useState<ImportResolveLine[] | null>(null);
  const [choices, setChoices] = useState<Map<number, LineChoice>>(new Map());
  const [result, setResult] = useState<{ added: number; updated: number } | null>(null);
  const resolve = useResolveImport();
  const importItems = useImportItems();

  const reset = () => {
    setText("");
    setLines(null);
    setChoices(new Map());
    setResult(null);
  };
  const close = () => {
    reset();
    onClose();
  };

  const matched = lines?.filter((l) => l.status === "matched") ?? [];
  const ambiguous = lines?.filter((l) => l.status === "ambiguous") ?? [];
  const unmatched = lines?.filter((l) => l.status === "unmatched") ?? [];
  const items = lines ? buildImportItems(lines, choices) : [];

  return (
    <Dialog open={open} onClose={close} title={m.collection_import_title()}>
      {result !== null ? (
        <div className="flex flex-col gap-4">
          {/* eslint-disable-next-line jsx-a11y/prefer-tag-over-role -- Alert renders a div; role="status" (not "alert") is intentional so success is announced politely */}
          <Alert variant="default" role="status">
            {m.collection_import_result({ added: result.added, updated: result.updated })}
          </Alert>
          <Button type="button" onClick={close}>
            {m.dialog_close()}
          </Button>
        </div>
      ) : lines === null ? (
        <form
          className="flex flex-col gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            resolve.mutate(
              { text },
              {
                onSuccess: (resolved) => {
                  setLines(resolved);
                  setChoices(defaultChoices(resolved));
                },
              },
            );
          }}
        >
          <p className="text-sm text-fg-muted">{m.collection_import_hint()}</p>
          <Label htmlFor="import-text">{m.collection_import_text_label()}</Label>
          <textarea
            id="import-text"
            required
            rows={10}
            value={text}
            onChange={(e) => setText(e.target.value)}
            className="rounded-md border border-border bg-surface p-2 font-mono text-sm text-fg"
          />
          {resolve.isError && <Alert variant="danger">{resolve.error.message}</Alert>}
          <Button type="submit" disabled={resolve.isPending}>
            {m.collection_import_resolve_button()}
          </Button>
        </form>
      ) : (
        <div className="flex flex-col gap-4">
          {matched.length > 0 && (
            <section>
              <h3 className="text-sm font-semibold text-fg">
                {m.collection_import_matched({ count: matched.length })}
              </h3>
              <ul className="text-sm text-fg-muted">
                {matched.map((l) => (
                  <li key={l.lineNumber}>
                    {l.quantity}× {l.match?.name}
                  </li>
                ))}
              </ul>
            </section>
          )}
          {ambiguous.length > 0 && (
            <section>
              <h3 className="text-sm font-semibold text-fg">
                {m.collection_import_ambiguous({ count: ambiguous.length })}
              </h3>
              <ul className="flex flex-col gap-2">
                {ambiguous.map((l) => {
                  const selectId = `import-choice-${l.lineNumber}`;
                  return (
                    <li key={l.lineNumber} className="flex flex-col gap-1">
                      <Label htmlFor={selectId}>
                        {m.collection_import_choice_label({ raw: l.raw })}
                      </Label>
                      <select
                        id={selectId}
                        value={choices.get(l.lineNumber) ?? ""}
                        onChange={(e) =>
                          setChoices((prev) =>
                            new Map(prev).set(
                              l.lineNumber,
                              e.target.value === "" ? null : e.target.value,
                            ),
                          )
                        }
                        className="rounded-md border border-border bg-surface p-1.5 text-sm text-fg"
                      >
                        {(l.suggestions ?? []).map((s) => (
                          <option key={s.scryfallId} value={s.scryfallId}>
                            {s.name} ({s.setName} · #{s.collectorNumber})
                          </option>
                        ))}
                        <option value="">{m.collection_import_skip()}</option>
                      </select>
                    </li>
                  );
                })}
              </ul>
            </section>
          )}
          {unmatched.length > 0 && (
            <section>
              <h3 className="text-sm font-semibold text-fg">
                {m.collection_import_unmatched({ count: unmatched.length })}
              </h3>
              <ul className="text-sm text-fg-muted">
                {unmatched.map((l) => (
                  <li key={l.lineNumber}>{l.raw}</li>
                ))}
              </ul>
            </section>
          )}
          {importItems.isError && <Alert variant="danger">{importItems.error.message}</Alert>}
          <div className="flex items-center gap-2">
            <Button type="button" variant="outline" onClick={() => setLines(null)}>
              {m.collection_import_back()}
            </Button>
            {items.length === 0 ? (
              <p className="text-sm text-fg-muted">{m.collection_import_nothing()}</p>
            ) : (
              <Button
                type="button"
                disabled={importItems.isPending}
                onClick={() =>
                  importItems.mutate(
                    { items },
                    {
                      onSuccess: (r) => setResult({ added: r.addedRows, updated: r.updatedRows }),
                    },
                  )
                }
              >
                {m.collection_import_confirm({ count: items.length })}
              </Button>
            )}
          </div>
        </div>
      )}
    </Dialog>
  );
}
