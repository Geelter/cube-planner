import { m } from "@/paraglide/messages";
import { useMediaQuery } from "@/shared/lib/useMediaQuery";
import { Alert } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Dialog } from "@/shared/ui/dialog";
import { Drawer } from "@/shared/ui/drawer";
import { useCardPrintings } from "./api";
import { ManaCost } from "./ManaCost";

export type PreviewCard = { oracleId: string; scryfallId?: string; name: string };

// Full card inspector, reachable by tap/Enter on card rows (issue #8).
// Desktop gets a centered dialog, phones a bottom sheet. Data comes from the
// printings query — same key PrintingPickerDialog uses, so it's cached
// across open/close cycles and across the two components.
export function CardPreviewSheet({
  card,
  onClose,
  onChangePrinting,
}: {
  card: PreviewCard;
  onClose: () => void;
  onChangePrinting?: (card: PreviewCard) => void;
}) {
  const isDesktop = useMediaQuery("(min-width: 768px)");
  const printings = useCardPrintings(card.oracleId);
  const shown =
    printings.data?.find((p) => p.scryfallId === card.scryfallId) ?? printings.data?.[0];

  const body = (
    <>
      {printings.isPending && <p className="text-sm text-fg-muted">{m.loading()}</p>}
      {printings.isError && <Alert variant="danger">{printings.error.message}</Alert>}
      {shown && (
        <div className="flex flex-col gap-6 overflow-y-auto md:flex-row">
          <div className="flex shrink-0 flex-col gap-2">
            {shown.imageNormal != null && (
              <img src={shown.imageNormal} alt={shown.name} className="w-64 rounded-xl" />
            )}
            {shown.backImageNormal != null && (
              <img
                src={shown.backImageNormal}
                alt={m.cards_preview_back_face({ name: shown.name })}
                className="w-64 rounded-xl"
              />
            )}
          </div>
          <div className="flex max-w-md flex-col gap-2">
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
            {onChangePrinting && (
              <div>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => onChangePrinting(card)}
                >
                  {m.cards_change_printing()}
                </Button>
              </div>
            )}
          </div>
        </div>
      )}
    </>
  );

  return isDesktop ? (
    <Dialog open onClose={onClose} title={card.name}>
      {body}
    </Dialog>
  ) : (
    <Drawer open onClose={onClose} label={card.name} side="bottom">
      {body}
    </Drawer>
  );
}
