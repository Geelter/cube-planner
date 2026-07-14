import { createFileRoute } from "@tanstack/react-router";
import { EventsListPage } from "@/features/events/components/EventsListPage";

export const Route = createFileRoute("/events/")({ component: EventsListPage });
