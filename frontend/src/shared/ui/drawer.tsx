import { cva, type VariantProps } from "class-variance-authority";
import { useEffect, useRef } from "react";
import type { ReactNode } from "react";
import { m } from "@/paraglide/messages";
import { Button } from "@/shared/ui/button";

// Positioning trick on both sides: the UA gives a modal <dialog> inset:0
// with auto margins; zeroing all margins and re-adding a single auto
// margin pins it to the opposite edge.
const drawerVariants = cva(
  "fixed m-0 border-border bg-surface p-4 text-fg shadow-lg backdrop:bg-overlay",
  {
    variants: {
      side: {
        right: "mr-0 ml-auto h-dvh max-h-none w-72 max-w-[80vw] border-l",
        bottom: "mt-auto max-h-[85svh] w-full max-w-none overflow-y-auto rounded-t-xl border-t",
      },
    },
    defaultVariants: { side: "right" },
  },
);

// Sheet on the native <dialog> element (same foundation as Dialog):
// showModal() provides the focus trap, Esc-to-close (fires the close
// event), ::backdrop, and focus restoration to the opener.
export function Drawer({
  open,
  onClose,
  label,
  children,
  id,
  side,
}: {
  open: boolean;
  onClose: () => void;
  label: string;
  children: ReactNode;
  id?: string;
  side?: VariantProps<typeof drawerVariants>["side"];
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
    // Native <dialog> backdrop: clicking the dialog element itself (not a
    // child) hits the backdrop area, so this dismisses on backdrop tap.
    // Esc-to-close is already provided natively; no keyboard equivalent is
    // needed for the backdrop specifically.
    // oxlint-disable-next-line jsx-a11y/click-events-have-key-events, jsx-a11y/no-noninteractive-element-interactions
    <dialog
      ref={ref}
      id={id}
      aria-label={label}
      onClose={onClose}
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
      className={drawerVariants({ side })}
    >
      {open && (
        <div className="flex h-full flex-col gap-2 overflow-y-auto">
          <div className="flex justify-end">
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="size-11"
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
