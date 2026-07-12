import { createFileRoute } from "@tanstack/react-router";
import { CubeBrowserPage } from "@/features/cubes/components/CubeBrowserPage";

export const Route = createFileRoute("/cubes/")({ component: CubeBrowserPage });
