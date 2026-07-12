import { createFileRoute } from "@tanstack/react-router";
import { MyCubesPage } from "@/features/cubes/components/MyCubesPage";

export const Route = createFileRoute("/cubes/mine")({ component: MyCubesPage });
