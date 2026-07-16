import { createFileRoute } from "@tanstack/react-router";
import { EventDetailPage } from "@/features/events/components/EventDetailPage";
import { TournamentSection } from "@/features/tournaments/components/TournamentSection";

function EventDetailRoute() {
  const { eventId } = Route.useParams();
  return (
    <div className="flex flex-col gap-8">
      <EventDetailPage />
      <TournamentSection eventId={eventId} />
    </div>
  );
}

export const Route = createFileRoute("/events/$eventId/")({
  component: EventDetailRoute,
  validateSearch: (s: Record<string, unknown>): { checkout?: "success" | "cancelled" } => ({
    ...(s.checkout === "success" || s.checkout === "cancelled" ? { checkout: s.checkout } : {}),
  }),
});
