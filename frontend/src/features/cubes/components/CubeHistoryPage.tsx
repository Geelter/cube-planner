import { Link, getRouteApi } from "@tanstack/react-router";
import { useState } from "react";
import { m } from "@/paraglide/messages";
import { getLocale } from "@/paraglide/runtime";
import { Alert } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/shared/ui/card";
import { CUBES_PAGE_SIZE, useCube, useCubeChanges } from "../api";

const route = getRouteApi("/cubes/$cubeId/history");

export function CubeHistoryPage() {
  const { cubeId } = route.useParams();
  const [page, setPage] = useState(0);
  const cube = useCube(cubeId);
  const changes = useCubeChanges(cubeId, page);

  if (cube.isPending || changes.isPending)
    return <p className="text-sm text-fg-muted">{m.loading()}</p>;
  if (cube.isError) return <Alert variant="danger">{cube.error.message}</Alert>;
  if (changes.isError) return <Alert variant="danger">{changes.error.message}</Alert>;

  const totalPages = Math.ceil(changes.data.total / CUBES_PAGE_SIZE);

  return (
    <div className="flex flex-col gap-6">
      <h1 className="text-2xl font-semibold text-fg">
        {m.cubes_history_title({ name: cube.data.name })}
      </h1>
      {changes.data.changes.length === 0 && (
        <p className="text-sm text-fg-muted">{m.cubes_history_empty()}</p>
      )}
      <ul className="flex flex-col gap-3">
        {changes.data.changes.map((change) => (
          <li key={change.id}>
            <Card>
              <CardHeader>
                <CardTitle as="h2" className="text-base">
                  {m.cubes_version_badge({ version: change.version })}
                  {change.note !== "" && <> — {change.note}</>}
                </CardTitle>
                <p className="text-sm text-fg-muted">
                  {change.authorName} · {new Date(change.createdAt).toLocaleString(getLocale())}
                </p>
              </CardHeader>
              <CardContent>
                <div className="flex flex-wrap gap-1.5">
                  {(change.adds ?? []).map((item) => (
                    <span
                      key={`add-${item.oracleId}`}
                      className="rounded-full bg-accent/10 px-2 py-0.5 text-xs text-fg"
                    >
                      +{item.quantity > 1 ? `${item.quantity} ` : ""}
                      {item.name}
                    </span>
                  ))}
                  {(change.removes ?? []).map((item) => (
                    <span
                      key={`rm-${item.oracleId}`}
                      className="rounded-full bg-danger/10 px-2 py-0.5 text-xs text-fg line-through"
                    >
                      −{item.quantity > 1 ? `${item.quantity} ` : ""}
                      {item.name}
                    </span>
                  ))}
                </div>
                <p className="mt-2">
                  <Link
                    to="/cubes/$cubeId"
                    params={{ cubeId }}
                    search={{ atVersion: change.version }}
                    className="text-sm text-accent hover:underline"
                  >
                    {m.cubes_history_view_version()}
                  </Link>
                </p>
              </CardContent>
            </Card>
          </li>
        ))}
      </ul>
      {totalPages > 1 && (
        <div className="flex items-center gap-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            aria-label={m.pagination_prev()}
            disabled={page === 0}
            onClick={() => setPage((p) => p - 1)}
          >
            ←
          </Button>
          <span className="text-sm text-fg-muted">
            {page + 1} / {totalPages}
          </span>
          <Button
            type="button"
            variant="outline"
            size="sm"
            aria-label={m.pagination_next()}
            disabled={page + 1 >= totalPages}
            onClick={() => setPage((p) => p + 1)}
          >
            →
          </Button>
        </div>
      )}
    </div>
  );
}
