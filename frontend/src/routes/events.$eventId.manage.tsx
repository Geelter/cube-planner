import { createFileRoute } from "@tanstack/react-router";
import { ManageEventPage } from "@/features/events/components/ManageEventPage";
import { TournamentPanel } from "@/features/tournaments/components/TournamentPanel";

function ManageEventRoute() {
  const { eventId } = Route.useParams();
  return (
    <div className="flex flex-col gap-8">
      <ManageEventPage />
      <TournamentPanel eventId={eventId} />
    </div>
  );
}

export const Route = createFileRoute("/events/$eventId/manage")({ component: ManageEventRoute });
