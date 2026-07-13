import { useState } from "react";
import type { ReactNode } from "react";
import type { CubeCardEntry } from "../api";

// Shows the card image in a floating panel while the row is hovered or
// focused (mouse hover or keyboard focus). Touch devices have no hover
// or focus trigger here, so the preview is not reachable by tap.
export function CardHoverPreview({ card, children }: { card: CubeCardEntry; children: ReactNode }) {
  const [open, setOpen] = useState(false);
  if (card.imageNormal == null) return <>{children}</>;
  return (
    <span
      className="relative inline-block w-full"
      onMouseEnter={() => setOpen(true)}
      onMouseLeave={() => setOpen(false)}
      onFocus={() => setOpen(true)}
      onBlur={() => setOpen(false)}
    >
      {children}
      {open && (
        <span className="pointer-events-none absolute top-full left-1/2 z-10 mt-1 block w-60 -translate-x-1/2">
          <img src={card.imageNormal} alt="" className="rounded-xl shadow-lg" />
        </span>
      )}
    </span>
  );
}
