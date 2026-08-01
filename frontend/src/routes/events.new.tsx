import { createFileRoute } from "@tanstack/react-router";
import { requireAuth } from "@/features/auth/guard";
import { NewEventPage } from "@/features/events/components/NewEventPage";

export const Route = createFileRoute("/events/new")({
  beforeLoad: requireAuth,
  component: NewEventPage,
});
