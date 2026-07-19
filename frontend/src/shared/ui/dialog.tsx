import { useEffect, useId, useRef } from "react";
import type { ReactNode } from "react";
import { m } from "@/paraglide/messages";
import { Button } from "@/shared/ui/button";

// Modal on top of the native <dialog> element: showModal() provides the
// focus trap, Esc-to-close (fires the close event), and ::backdrop.
export function Dialog({
  open,
  onClose,
  title,
  children,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  children: ReactNode;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    if (open && !el.open) {
      // Test environments may lack showModal — fall back to the open attr.
      if (typeof el.showModal === "function") el.showModal();
      else el.setAttribute("open", "");
    } else if (!open && el.open) {
      el.close();
    }
  }, [open]);
  return (
    <dialog
      ref={ref}
      aria-labelledby={titleId}
      onClose={onClose}
      className="m-auto max-h-[85svh] w-[calc(100%-2rem)] max-w-lg overflow-y-auto rounded-xl border border-border bg-surface p-6 text-fg shadow-lg backdrop:bg-overlay"
    >
      {open && (
        <div className="flex flex-col gap-4">
          <div className="flex items-start justify-between gap-4">
            <h2 id={titleId} className="text-lg font-semibold text-fg">
              {title}
            </h2>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              aria-label={m.dialog_close()}
              onClick={onClose}
            >
              ✕
            </Button>
          </div>
          {children}
        </div>
      )}
    </dialog>
  );
}
