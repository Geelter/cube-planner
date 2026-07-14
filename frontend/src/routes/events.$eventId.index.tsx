import { createFileRoute } from "@tanstack/react-router";
import { EventDetailPage } from "@/features/events/components/EventDetailPage";

export const Route = createFileRoute("/events/$eventId/")({
  component: EventDetailPage,
  validateSearch: (s: Record<string, unknown>): { checkout?: "success" | "cancelled" } => ({
    ...(s.checkout === "success" || s.checkout === "cancelled" ? { checkout: s.checkout } : {}),
  }),
});
