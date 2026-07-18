import { m } from "@/paraglide/messages";
import { Alert } from "@/shared/ui/alert";
import { Dialog } from "@/shared/ui/dialog";
import { useCardPrintings } from "./api";

export function PrintingPickerDialog({
  open,
  onClose,
  oracleId,
  name,
  currentScryfallId,
  onPick,
}: {
  open: boolean;
  onClose: () => void;
  oracleId: string;
  name: string;
  currentScryfallId: string;
  onPick: (scryfallId: string) => void;
}) {
  // Only fetch while open — the picker mounts once per row.
  const printings = useCardPrintings(open ? oracleId : undefined);
  return (
    <Dialog open={open} onClose={onClose} title={m.printing_picker_title({ name })}>
      {printings.isPending && <p className="text-sm text-fg-muted">{m.loading()}</p>}
      {printings.isError && <Alert variant="danger">{printings.error.message}</Alert>}
      {printings.data && (
        <ul className="flex max-h-96 flex-col gap-1 overflow-y-auto">
          {printings.data.map((p) => {
            const current = p.scryfallId === currentScryfallId;
            const setLine = m.cards_set_line({
              setName: p.setName,
              collectorNumber: p.collectorNumber,
            });
            return (
              <li key={p.scryfallId}>
                {current ? (
                  // A real, focusable <button> (not a visual-only <span>) so
                  // keyboard/AT users land on the current printing like they
                  // do every other row; aria-current announces it as such.
                  // Activating it means "keep what I have": close without
                  // picking (a same-printing pick is a service error anyway).
                  <button
                    type="button"
                    aria-current="true"
                    onClick={onClose}
                    className="flex w-full items-center gap-3 rounded-md bg-accent/10 px-2 py-1.5 text-left text-sm text-fg"
                  >
                    {p.imageSmall != null && (
                      <img src={p.imageSmall} alt="" className="h-12 rounded" />
                    )}
                    {setLine}
                    <span className="ml-auto text-xs font-semibold text-accent">
                      {m.printing_picker_current()}
                    </span>
                  </button>
                ) : (
                  <button
                    type="button"
                    className="flex w-full items-center gap-3 rounded-md px-2 py-1.5 text-left text-sm text-fg hover:bg-surface-raised focus-visible:outline-2"
                    onClick={() => onPick(p.scryfallId)}
                  >
                    {p.imageSmall != null && (
                      <img src={p.imageSmall} alt="" className="h-12 rounded" />
                    )}
                    {setLine}
                  </button>
                )}
              </li>
            );
          })}
        </ul>
      )}
    </Dialog>
  );
}
