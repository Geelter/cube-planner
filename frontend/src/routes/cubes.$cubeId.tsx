import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/cubes/$cubeId")({
  component: () => <div>Cube detail page (Task 10)</div>,
});
