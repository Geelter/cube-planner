import { createFileRoute, Outlet } from "@tanstack/react-router";

export const Route = createFileRoute("/cubes/$cubeId")({
  component: Outlet,
});
