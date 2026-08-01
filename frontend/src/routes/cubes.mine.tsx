import { createFileRoute } from "@tanstack/react-router";
import { requireAuth } from "@/features/auth/guard";
import { MyCubesPage } from "@/features/cubes/components/MyCubesPage";

export const Route = createFileRoute("/cubes/mine")({
  beforeLoad: requireAuth,
  component: MyCubesPage,
});
