import type { ComponentProps } from "react";
import { cn } from "@/shared/lib/cn";

export function Label({ className, ...props }: ComponentProps<"label">) {
  // eslint-disable-next-line jsx-a11y/label-has-associated-control
  return <label className={cn("text-sm font-medium text-fg", className)} {...props} />;
}
