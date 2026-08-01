import { createFileRoute } from "@tanstack/react-router";
import { requireAuth } from "@/features/auth/guard";
import { CollectionPage } from "@/features/collection/components/CollectionPage";

export const Route = createFileRoute("/collection")({
  beforeLoad: requireAuth,
  component: CollectionPage,
});
