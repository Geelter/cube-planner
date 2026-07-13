import { createFileRoute } from "@tanstack/react-router";
import { CubeEditorPage } from "@/features/cubes/components/CubeEditorPage";

export const Route = createFileRoute("/cubes/$cubeId/edit")({ component: CubeEditorPage });
