import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/cubes/$cubeId/edit")({
  component: () => <div>Cube editor (Task 11)</div>,
});
