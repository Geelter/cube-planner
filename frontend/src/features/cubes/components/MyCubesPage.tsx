import { Link } from "@tanstack/react-router";
import { m } from "@/paraglide/messages";
import { Alert } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { useMyCubes } from "../api";
import { CubeListItem } from "./CubeListItem";

export function MyCubesPage() {
  const mine = useMyCubes();

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between gap-4">
        <h1 className="text-2xl font-semibold text-fg">{m.cubes_mine_title()}</h1>
        <Button asChild size="sm">
          <Link to="/cubes/new">{m.cubes_new_button()}</Link>
        </Button>
      </div>
      {mine.isPending && <p className="text-sm text-fg-muted">{m.loading()}</p>}
      {mine.isError && <Alert variant="danger">{mine.error.message}</Alert>}
      {mine.data && mine.data.length === 0 && (
        <p className="text-sm text-fg-muted">{m.cubes_mine_empty()}</p>
      )}
      {mine.data && mine.data.length > 0 && (
        <ul className="flex flex-col gap-3">
          {mine.data.map((cube) => (
            <li key={cube.id}>
              <CubeListItem cube={cube} />
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
