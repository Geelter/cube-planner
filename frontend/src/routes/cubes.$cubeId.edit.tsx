import { createFileRoute } from "@tanstack/react-router";
import { requireAuth } from "@/features/auth/guard";
import { CubeEditorPage } from "@/features/cubes/components/CubeEditorPage";

export const Route = createFileRoute("/cubes/$cubeId/edit")({
  beforeLoad: requireAuth,
  component: CubeEditorPage,
});
