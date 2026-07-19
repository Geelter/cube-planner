import { m } from "@/paraglide/messages";
import { Button } from "@/shared/ui/button";
import type { PendingState } from "../lib/pendingDiff";
import { pendingTotals } from "../lib/pendingDiff";

// Collapsed summary of the pending diff, fixed to the viewport bottom
// below lg where the full panel stacks out of reach under a long card
// list. Rendered only while there are pending changes.
export function PendingChangesBar({
  pending,
  sheetId,
  onExpand,
  onSave,
  saving,
}: {
  pending: PendingState;
  sheetId: string;
  onExpand: () => void;
  onSave: () => void;
  saving: boolean;
}) {
  const totals = pendingTotals(pending);
  return (
    <section
      aria-label={m.cubes_pending_title()}
      className="fixed inset-x-0 bottom-0 z-10 border-t border-border bg-surface-raised shadow-lg lg:hidden"
    >
      <div className="mx-auto flex max-w-4xl items-center justify-between gap-3 p-3 pb-[max(0.75rem,env(safe-area-inset-bottom))]">
        <button
          type="button"
          aria-label={m.cubes_pending_bar_review({ adds: totals.adds, removes: totals.removes })}
          aria-haspopup="dialog"
          aria-controls={sheetId}
          onClick={onExpand}
          className="flex h-11 min-w-0 flex-1 items-center gap-3 rounded-md px-2 text-sm hover:bg-surface"
        >
          <span className="font-semibold text-accent">+{totals.adds}</span>
          <span className="font-semibold text-danger">−{totals.removes}</span>
        </button>
        <Button type="button" size="lg" disabled={saving} onClick={onSave}>
          {m.cubes_save_changes()}
        </Button>
      </div>
    </section>
  );
}
