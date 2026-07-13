import { createFileRoute } from "@tanstack/react-router";
import { WantlistPage } from "@/features/collection/components/WantlistPage";

export const Route = createFileRoute("/cubes/$cubeId/wantlist")({ component: WantlistPage });
