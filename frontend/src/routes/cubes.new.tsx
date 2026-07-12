import { createFileRoute } from "@tanstack/react-router";
import { CreateCubePage } from "@/features/cubes/components/CreateCubePage";

export const Route = createFileRoute("/cubes/new")({ component: CreateCubePage });
