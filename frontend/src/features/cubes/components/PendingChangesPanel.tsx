import { useId } from "react";
import { m } from "@/paraglide/messages";
import { Button } from "@/shared/ui/button";
import { Label } from "@/shared/ui/label";
import type { PendingAction, PendingState } from "../lib/pendingDiff";
import { pendingCount } from "../lib/pendingDiff";

const panelVariants = {
  // Page flow: hidden below lg (the bar + sheet take over), side column at lg.
  page: "hidden w-full flex-col gap-3 rounded-lg border border-border bg-surface-raised p-4 lg:flex lg:w-72",
  // Inside the bottom sheet: the Drawer already provides padding and chrome.
  sheet: "flex w-full flex-col gap-3",
} as const;

export function PendingChangesPanel({
  pending,
  dispatch,
  note,
  onNoteChange,
  onSave,
  onDiscard,
  saving,
  variant = "page",
}: {
  pending: PendingState;
  dispatch: (action: PendingAction) => void;
  note: string;
  onNoteChange: (v: string) => void;
  onSave: () => void;
  onDiscard: () => void;
  saving: boolean;
  variant?: keyof typeof panelVariants;
}) {
  const count = pendingCount(pending);
  const noteId = useId();
  return (
    <aside className={panelVariants[variant]}>
      <h2 className="text-sm font-semibold text-fg">{m.cubes_pending_title()}</h2>
      {count === 0 && <p className="text-sm text-fg-muted">{m.cubes_pending_empty()}</p>}
      {pending.adds.size > 0 && (
        <div>
          <h3 className="mb-1 text-xs font-medium text-fg-muted uppercase">
            {m.cubes_pending_adds()}
          </h3>
          <ul className="flex flex-col gap-1">
            {[...pending.adds.values()].map(({ card, quantity }) => (
              <li key={card.oracleId} className="flex items-center justify-between gap-2 text-sm">
                <span className="truncate text-fg">
                  <span className="font-semibold text-accent">+{quantity}</span> {card.name}
                </span>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => dispatch({ type: "undoAdd", oracleId: card.oracleId })}
                >
                  {m.cubes_pending_undo()}
                </Button>
              </li>
            ))}
          </ul>
        </div>
      )}
      {pending.removes.size > 0 && (
        <div>
          <h3 className="mb-1 text-xs font-medium text-fg-muted uppercase">
            {m.cubes_pending_removes()}
          </h3>
          <ul className="flex flex-col gap-1">
            {[...pending.removes.entries()].map(([oracleId, { entry, quantity }]) => (
              <li key={oracleId} className="flex items-center justify-between gap-2 text-sm">
                <span className="truncate text-fg">
                  <span className="font-semibold text-danger">−{quantity}</span> {entry.name}
                </span>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => dispatch({ type: "undoRemove", oracleId })}
                >
                  {m.cubes_pending_undo()}
                </Button>
              </li>
            ))}
          </ul>
        </div>
      )}
      <div className="flex flex-col gap-1.5">
        <Label htmlFor={noteId}>{m.cubes_note_label()}</Label>
        <textarea
          id={noteId}
          className="min-h-16 rounded-md border border-border bg-surface px-3 py-2 text-sm text-fg"
          maxLength={500}
          value={note}
          onChange={(e) => onNoteChange(e.target.value)}
        />
      </div>
      <div className="flex gap-2">
        <Button type="button" disabled={count === 0 || saving} onClick={onSave}>
          {m.cubes_save_changes()}
        </Button>
        <Button
          type="button"
          variant="outline"
          disabled={count === 0 || saving}
          onClick={onDiscard}
        >
          {m.cubes_discard_changes()}
        </Button>
      </div>
    </aside>
  );
}
