import { createFileRoute } from "@tanstack/react-router";
import { ManageEventPage } from "@/features/events/components/ManageEventPage";

export const Route = createFileRoute("/events/$eventId/manage")({ component: ManageEventPage });
