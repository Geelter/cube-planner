import { type Dispatch, type SetStateAction, useState } from "react";
import { m } from "@/paraglide/messages";
import { Alert } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Dialog } from "@/shared/ui/dialog";
import { Label } from "@/shared/ui/label";
import type { CardSummary } from "./api";
import { buildImportItems, defaultChoices } from "./listImportReview";
import type { LineChoice } from "./listImportReview";
import { useResolveCardList } from "./useResolveCardList";
import type { ImportCardMatch, ImportResolveLine } from "./useResolveCardList";

// The card, not just its id: the resolved lines already carry a full CardRef,
// and the cube editor's pending diff needs the card to render staged rows.
// Callers that only need an id (collection's commit) map down themselves.
export type ResolvedItem = { card: CardSummary; quantity: number };

function cardSummaryFromMatch(match: ImportCardMatch): CardSummary {
  return {
    scryfallId: match.scryfallId,
    oracleId: match.oracleId,
    name: match.name,
    manaCost: match.manaCost,
    typeLine: match.typeLine,
    colors: match.colors ?? [],
    imageSmall: match.imageSmall,
  };
}

// Echoes what the parser understood from the pasted selector, e.g.
// " (ATQ) 83a" or " (ATQ)" — empty when the line had no set selector.
function parsedSelector(l: { setCode: string; collectorNumber: string }): string {
  if (!l.setCode) return "";
  const set = l.setCode.toUpperCase();
  return l.collectorNumber ? ` (${set}) ${l.collectorNumber}` : ` (${set})`;
}

// Shared by the ambiguous and printing-not-found groups: both offer the
// same kind of choice (pick a printing from suggestions, or skip).
function ChoiceSection({
  heading,
  lines,
  choices,
  setChoices,
}: {
  heading: string;
  lines: ImportResolveLine[];
  choices: Map<number, LineChoice>;
  setChoices: Dispatch<SetStateAction<Map<number, LineChoice>>>;
}) {
  return (
    <section>
      <h3 className="text-sm font-semibold text-fg">{heading}</h3>
      <ul className="flex flex-col gap-2">
        {lines.map((l) => {
          const selectId = `import-choice-${l.lineNumber}`;
          return (
            <li key={l.lineNumber} className="flex flex-col gap-1">
              <Label htmlFor={selectId}>{m.collection_import_choice_label({ raw: l.raw })}</Label>
              <select
                id={selectId}
                value={choices.get(l.lineNumber) ?? ""}
                onChange={(e) =>
                  setChoices((prev) =>
                    new Map(prev).set(l.lineNumber, e.target.value === "" ? null : e.target.value),
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
  );
}

function toResolvedItems(
  lines: ImportResolveLine[],
  choices: Map<number, LineChoice>,
): ResolvedItem[] {
  const matchByScryfallId = new Map<string, ImportCardMatch>();
  for (const line of lines) {
    if (line.match) matchByScryfallId.set(line.match.scryfallId, line.match);
    for (const s of line.suggestions ?? []) matchByScryfallId.set(s.scryfallId, s);
  }
  return buildImportItems(lines, choices).flatMap(({ scryfallId, quantity }) => {
    const match = matchByScryfallId.get(scryfallId);
    return match ? [{ card: cardSummaryFromMatch(match), quantity }] : [];
  });
}

export function CardListImportDialog({
  open,
  onClose,
  onApply,
  applying,
  applyError,
  result = null,
  initialLines,
  confirmLabel,
}: {
  open: boolean;
  onClose: () => void;
  onApply: (items: ResolvedItem[]) => void;
  applying?: boolean;
  applyError?: Error | null;
  result?: { added: number; updated: number } | null;
  /** Seed the review phase directly, skipping the paste phase — used by the
   *  create-cube flow, which resolves before the cube exists (Task 21).
   *  Must be present at mount: this dialog is meant to be conditionally
   *  mounted (like other seeded dialogs in this codebase) rather than kept
   *  around with a changing `initialLines`. */
  initialLines?: ImportResolveLine[];
  /** Confirm-button label for the review step, given the resolved item
   *  count. Defaults to the collection's "Add to collection" copy; callers
   *  staging into a cube (editor or create-cube flow) must pass their own
   *  so the button doesn't lie about where the cards are going. */
  confirmLabel?: (args: { count: number }) => string;
}) {
  const seeded = initialLines !== undefined;
  const [text, setText] = useState("");
  const [lines, setLines] = useState<ImportResolveLine[] | null>(initialLines ?? null);
  const [choices, setChoices] = useState<Map<number, LineChoice>>(
    initialLines ? defaultChoices(initialLines) : new Map(),
  );
  const resolve = useResolveCardList();

  const reset = () => {
    setText("");
    setLines(initialLines ?? null);
    setChoices(initialLines ? defaultChoices(initialLines) : new Map());
  };
  const close = () => {
    reset();
    onClose();
  };

  const matched = lines?.filter((l) => l.status === "matched") ?? [];
  const ambiguous = lines?.filter((l) => l.status === "ambiguous") ?? [];
  const printingNotFound = lines?.filter((l) => l.status === "printing-not-found") ?? [];
  const unmatched = lines?.filter((l) => l.status === "unmatched") ?? [];
  const items = lines ? toResolvedItems(lines, choices) : [];

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
          <Button type="submit" loading={resolve.isPending}>
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
                    {parsedSelector(l)}
                  </li>
                ))}
              </ul>
            </section>
          )}
          {ambiguous.length > 0 && (
            <ChoiceSection
              heading={m.collection_import_ambiguous({ count: ambiguous.length })}
              lines={ambiguous}
              choices={choices}
              setChoices={setChoices}
            />
          )}
          {printingNotFound.length > 0 && (
            <ChoiceSection
              heading={m.collection_import_printing_not_found({ count: printingNotFound.length })}
              lines={printingNotFound}
              choices={choices}
              setChoices={setChoices}
            />
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
          {applyError && <Alert variant="danger">{applyError.message}</Alert>}
          <div className="flex items-center gap-2">
            <Button
              type="button"
              variant="outline"
              onClick={() => (seeded ? close() : setLines(null))}
            >
              {m.collection_import_back()}
            </Button>
            {items.length === 0 ? (
              <p className="text-sm text-fg-muted">{m.collection_import_nothing()}</p>
            ) : (
              <Button type="button" loading={applying === true} onClick={() => onApply(items)}>
                {(confirmLabel ?? m.collection_import_confirm)({ count: items.length })}
              </Button>
            )}
          </div>
        </div>
      )}
    </Dialog>
  );
}
