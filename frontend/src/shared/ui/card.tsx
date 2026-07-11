import type { ComponentProps } from "react";
import { cn } from "@/shared/lib/cn";

export function Card({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      className={cn("rounded-xl border border-border bg-surface-raised shadow-sm", className)}
      {...props}
    />
  );
}

export function CardHeader({ className, ...props }: ComponentProps<"div">) {
  return <div className={cn("flex flex-col gap-1.5 p-6", className)} {...props} />;
}

type CardTitleProps = ComponentProps<"h2"> & { as?: "h1" | "h2" | "h3" };

export function CardTitle({ className, as: Comp = "h2", ...props }: CardTitleProps) {
  return <Comp className={cn("text-lg leading-none font-semibold", className)} {...props} />;
}

export function CardContent({ className, ...props }: ComponentProps<"div">) {
  return <div className={cn("p-6 pt-0", className)} {...props} />;
}

export function CardFooter({ className, ...props }: ComponentProps<"div">) {
  return <div className={cn("flex items-center p-6 pt-0", className)} {...props} />;
}
