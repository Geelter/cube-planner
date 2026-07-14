import { createFileRoute } from "@tanstack/react-router";
import { NewEventPage } from "@/features/events/components/NewEventPage";

export const Route = createFileRoute("/events/new")({ component: NewEventPage });
