import { useId, useState, type KeyboardEvent, type ReactNode } from "react";
import { cn } from "@/shared/lib/cn";
import { Input } from "@/shared/ui/input";

type ComboboxProps<T> = {
  id: string;
  value: string;
  onValueChange: (value: string) => void;
  options: readonly T[];
  getOptionId: (option: T) => string;
  renderOption: (option: T) => ReactNode;
  onSelect: (option: T) => void;
  loading?: boolean;
  /** Minimum input length before the list opens (default 0). */
  minChars?: number;
  emptyMessage: string;
  loadingMessage: string;
  placeholder?: string;
  className?: string;
};

/**
 * Accessible async autocomplete (ARIA combobox pattern): the consumer owns
 * the input value and the option list; this component owns open state,
 * keyboard navigation, and listbox semantics.
 */
export function Combobox<T>({
  id,
  value,
  onValueChange,
  options,
  getOptionId,
  renderOption,
  onSelect,
  loading = false,
  minChars = 0,
  emptyMessage,
  loadingMessage,
  placeholder,
  className,
}: ComboboxProps<T>) {
  const [open, setOpen] = useState(false);
  const [activeIndex, setActiveIndex] = useState(-1);
  const listboxId = useId();

  const showList = open && value.trim().length >= minChars;
  const activeOption = activeIndex >= 0 ? options[activeIndex] : undefined;

  function close() {
    setOpen(false);
    setActiveIndex(-1);
  }

  function select(option: T) {
    onSelect(option);
    close();
  }

  function handleKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    switch (e.key) {
      case "ArrowDown":
        e.preventDefault();
        setOpen(true);
        setActiveIndex((i) => Math.min(i + 1, options.length - 1));
        break;
      case "ArrowUp":
        e.preventDefault();
        setActiveIndex((i) => Math.max(i - 1, 0));
        break;
      case "Enter":
        if (showList && activeOption !== undefined) {
          e.preventDefault();
          select(activeOption);
        }
        break;
      case "Escape":
        close();
        break;
      default:
        break;
    }
  }

  // WAI-ARIA combobox pattern (APG): `Input` renders a focusable <input>, the
  // popup is a ul/li listbox, and keyboard interaction lives on the input —
  // these jsx-a11y rules cannot see through the custom component or the
  // pattern, so they are scoped off for this component's JSX only.
  /* eslint-disable jsx-a11y/interactive-supports-focus, jsx-a11y/prefer-tag-over-role, jsx-a11y/no-noninteractive-element-to-interactive-role, jsx-a11y/click-events-have-key-events */
  return (
    <div className={cn("relative", className)}>
      <Input
        id={id}
        role="combobox"
        aria-expanded={showList}
        aria-controls={listboxId}
        aria-autocomplete="list"
        aria-activedescendant={
          showList && activeOption !== undefined
            ? `${listboxId}-${getOptionId(activeOption)}`
            : undefined
        }
        autoComplete="off"
        placeholder={placeholder}
        value={value}
        onChange={(e) => {
          onValueChange(e.target.value);
          setOpen(true);
          setActiveIndex(-1);
        }}
        onFocus={() => setOpen(true)}
        onBlur={close}
        onKeyDown={handleKeyDown}
      />
      {showList && (
        <ul
          id={listboxId}
          role="listbox"
          className="absolute z-10 mt-1 max-h-80 w-full overflow-auto rounded-md border border-border bg-surface-raised py-1 shadow-lg"
        >
          {options.length === 0 && (
            <li className="px-3 py-2 text-sm text-fg-muted" role="presentation">
              {loading ? loadingMessage : emptyMessage}
            </li>
          )}
          {options.map((option, i) => (
            <li
              key={getOptionId(option)}
              id={`${listboxId}-${getOptionId(option)}`}
              role="option"
              aria-selected={i === activeIndex}
              className={cn(
                "cursor-pointer px-3 py-2 text-sm text-fg",
                i === activeIndex && "bg-surface",
              )}
              // Prevent the input blur so click-to-select works.
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => select(option)}
              onMouseEnter={() => setActiveIndex(i)}
            >
              {renderOption(option)}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
