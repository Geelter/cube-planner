import { createFileRoute } from "@tanstack/react-router";
import { CubeDisplayPage } from "@/features/cubes/components/CubeDisplayPage";

export const Route = createFileRoute("/cubes/$cubeId/")({
  validateSearch: (search: Record<string, unknown>): { atVersion?: number } => {
    const raw = search["atVersion"];
    const n = typeof raw === "number" ? raw : typeof raw === "string" ? Number(raw) : NaN;
    return Number.isInteger(n) && n >= 0 ? { atVersion: n } : {};
  },
  component: CubeDisplayPage,
});
