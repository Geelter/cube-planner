import { LoaderIcon } from "lucide-react";
import type { ComponentProps } from "react";
import { m } from "@/paraglide/messages";
import { cn } from "@/shared/lib/cn";

export function Spinner({ className, ...props }: ComponentProps<"svg">) {
  return (
    <LoaderIcon
      // eslint-disable-next-line jsx-a11y/prefer-tag-over-role
      role="status"
      aria-label={m.loading()}
      className={cn("size-4 animate-spin", className)}
      {...props}
    />
  );
}
