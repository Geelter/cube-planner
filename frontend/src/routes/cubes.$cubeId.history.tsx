import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/cubes/$cubeId/history")({
  component: () => <div>Cube history (Task 12)</div>,
});
