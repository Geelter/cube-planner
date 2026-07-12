import { Link } from "@tanstack/react-router";
import { m } from "@/paraglide/messages";
import { getLocale } from "@/paraglide/runtime";
import { Card, CardContent, CardHeader, CardTitle } from "@/shared/ui/card";
import type { CubeSummary } from "../api";

export function CubeListItem({ cube }: { cube: CubeSummary }) {
  const updated = new Date(cube.updatedAt).toLocaleDateString(getLocale());
  return (
    <Card>
      <CardHeader>
        <CardTitle as="h2">
          <Link
            to="/cubes/$cubeId"
            params={{ cubeId: cube.id }}
            className="hover:text-accent hover:underline"
          >
            {cube.name}
          </Link>
        </CardTitle>
        <p className="text-sm text-fg-muted">
          {m.cubes_by_owner({ owner: cube.ownerName })}
          {cube.visibility === "private" && <> · {m.cubes_visibility_private()}</>}
        </p>
      </CardHeader>
      <CardContent>
        {cube.description !== "" && <p className="mb-2 text-sm text-fg">{cube.description}</p>}
        <p className="text-sm text-fg-muted">
          {m.cubes_card_count({ count: cube.cardCount })} · {m.cubes_updated({ date: updated })}
        </p>
      </CardContent>
    </Card>
  );
}
