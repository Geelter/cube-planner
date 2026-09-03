import { useEffect } from "react";

// Module-scoped because the lock is a property of the document, not of
// any one component: nested overlays (CardPreviewSheet over
// PrintingPickerDialog) must not unlock the body while an outer one is
// still open, so the last release wins rather than the first.
let lockCount = 0;
let restore: { overflow: string; paddingRight: string; scrollY: number } | null = null;

function acquire() {
  lockCount += 1;
  if (lockCount > 1) return;
  const { body } = document;
  restore = {
    overflow: body.style.overflow,
    paddingRight: body.style.paddingRight,
    scrollY: window.scrollY,
  };
  // Compensate for the scrollbar the lock removes, so fixed-width
  // layouts don't jump sideways on desktop.
  const gap = window.innerWidth - document.documentElement.clientWidth;
  body.style.overflow = "hidden";
  if (gap > 0) body.style.paddingRight = `${gap}px`;
}

function release() {
  lockCount -= 1;
  if (lockCount > 0 || restore === null) return;
  const { body } = document;
  body.style.overflow = restore.overflow;
  body.style.paddingRight = restore.paddingRight;
  const { scrollY } = restore;
  restore = null;
  window.scrollTo(0, scrollY);
}

// Locks body scroll while `active`. Needed because iOS Safari does NOT
// suppress root scroll for a modal <dialog> the way desktop browsers do:
// without this the page scrolls and rubber-bands behind bottom sheets.
export function useScrollLock(active: boolean): void {
  useEffect(() => {
    if (!active) return;
    acquire();
    return release;
  }, [active]);
}
