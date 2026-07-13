import { createFileRoute } from "@tanstack/react-router";
import { CubeHistoryPage } from "@/features/cubes/components/CubeHistoryPage";

export const Route = createFileRoute("/cubes/$cubeId/history")({ component: CubeHistoryPage });
