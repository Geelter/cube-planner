import { useEffect, useRef } from "react";
import type { ReactNode } from "react";
import { m } from "@/paraglide/messages";
import { Button } from "@/shared/ui/button";

// Right-side sheet on the native <dialog> element (same foundation as
// Dialog): showModal() provides the focus trap, Esc-to-close (fires the
// close event), ::backdrop, and focus restoration to the opener.
export function Drawer({
  open,
  onClose,
  label,
  children,
}: {
  open: boolean;
  onClose: () => void;
  label: string;
  children: ReactNode;
}) {
  const ref = useRef<HTMLDialogElement>(null);
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
      aria-label={label}
      onClose={onClose}
      className="fixed m-0 mr-0 ml-auto h-dvh max-h-none w-72 max-w-[80vw] border-l border-border bg-surface p-4 text-fg shadow-lg backdrop:bg-black/50"
    >
      {open && (
        <div className="flex h-full flex-col gap-2 overflow-y-auto">
          <div className="flex justify-end">
            <Button
              type="button"
              variant="ghost"
              size="icon"
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
