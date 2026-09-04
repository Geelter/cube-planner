import type { ReactNode } from "react";
import { m } from "@/paraglide/messages";
import { Button } from "@/shared/ui/button";
import { Dialog } from "@/shared/ui/dialog";

// The confirm-a-mutation shape, previously copy-pasted across events and
// tournaments. `pending` keeps the spinner visible: callers close from
// `onSettled`, never straight after `mutate()`.
export function ConfirmDialog({
  open,
  onClose,
  onConfirm,
  title,
  message,
  confirmLabel,
  pending,
  danger,
}: {
  open: boolean;
  onClose: () => void;
  onConfirm: () => void;
  title: string;
  message: string;
  confirmLabel: string;
  pending?: boolean;
  danger?: boolean;
}): ReactNode {
  return (
    <Dialog open={open} onClose={onClose} title={title}>
      <p className="text-sm text-fg">{message}</p>
      <div className="flex justify-end gap-2">
        <Button type="button" variant="ghost" onClick={onClose}>
          {m.dialog_close()}
        </Button>
        <Button
          type="button"
          variant={danger === true ? "danger" : "default"}
          loading={pending === true}
          onClick={onConfirm}
        >
          {confirmLabel}
        </Button>
      </div>
    </Dialog>
  );
}
