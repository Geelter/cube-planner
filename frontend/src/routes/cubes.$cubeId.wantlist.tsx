import { createFileRoute } from "@tanstack/react-router";
import { requireAuth } from "@/features/auth/guard";
import { WantlistPage } from "@/features/collection/components/WantlistPage";

export const Route = createFileRoute("/cubes/$cubeId/wantlist")({
  beforeLoad: requireAuth,
  component: WantlistPage,
});
