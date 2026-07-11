import { createFileRoute } from "@tanstack/react-router";
import { m } from "@/paraglide/messages";

export const Route = createFileRoute("/")({
  component: () => <h1 className="text-2xl font-semibold">{m.app_name()}</h1>,
});
