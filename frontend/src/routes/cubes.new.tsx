import { createFileRoute } from "@tanstack/react-router";
import { requireAuth } from "@/features/auth/guard";
import { CreateCubePage } from "@/features/cubes/components/CreateCubePage";

export const Route = createFileRoute("/cubes/new")({
  beforeLoad: requireAuth,
  component: CreateCubePage,
});
