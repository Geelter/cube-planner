import { createFileRoute, Outlet } from "@tanstack/react-router";

export const Route = createFileRoute("/cubes/$cubeId")({
  component: () => (
    <div className="mx-auto max-w-5xl px-4 py-6">
      <Outlet />
    </div>
  ),
});
