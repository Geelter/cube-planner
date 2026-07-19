import { useEffect, useRef, useState } from "react";
import { m } from "@/paraglide/messages";
import { Button } from "@/shared/ui/button";

const COMMIT_DELAY_MS = 400;

// Local-first stepper: clicks update the displayed value immediately and
// debounce into ONE onCommit with the final value. PUT is a set, so the
// final value winning is exactly right — no lost-update risk against
// yourself.
export function QuantityStepper({
  name,
  quantity,
  onCommit,
}: {
  name: string;
  quantity: number;
  onCommit: (quantity: number) => void;
}) {
  const [value, setValue] = useState(quantity);
  const committed = useRef(quantity);

  // Adopt server refetches (e.g. after an import bumped this row).
  useEffect(() => {
    setValue(quantity);
    committed.current = quantity;
  }, [quantity]);

  useEffect(() => {
    if (value === committed.current) return;
    const timer = setTimeout(() => {
      committed.current = value;
      onCommit(value);
    }, COMMIT_DELAY_MS);
    return () => clearTimeout(timer);
  }, [value, onCommit]);

  return (
    <span className="flex items-center gap-1">
      <Button
        type="button"
        variant="ghost"
        size="icon"
        className="size-11 text-base"
        aria-label={m.collection_qty_decrease({ name })}
        disabled={value <= 0}
        onClick={() => setValue((v) => Math.max(0, v - 1))}
      >
        −
      </Button>
      <span className="w-8 text-center text-sm text-fg tabular-nums">{value}</span>
      <Button
        type="button"
        variant="ghost"
        size="icon"
        className="size-11 text-base"
        aria-label={m.collection_qty_increase({ name })}
        disabled={value >= 999}
        onClick={() => setValue((v) => Math.min(999, v + 1))}
      >
        +
      </Button>
    </span>
  );
}
