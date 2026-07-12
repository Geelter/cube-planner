import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { m } from "@/paraglide/messages";
import { useDebouncedValue } from "@/shared/lib/useDebouncedValue";
import { Alert } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Input } from "@/shared/ui/input";
import { Label } from "@/shared/ui/label";
import { CUBES_PAGE_SIZE, useCubeList } from "../api";
import { CubeListItem } from "./CubeListItem";

export function CubeBrowserPage() {
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(0);
  const debounced = useDebouncedValue(search, 250);
  const list = useCubeList(debounced, page);

  const totalPages = list.data ? Math.ceil(list.data.total / CUBES_PAGE_SIZE) : 0;

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between gap-4">
        <h1 className="text-2xl font-semibold text-fg">{m.cubes_browser_title()}</h1>
        <Button asChild size="sm">
          <Link to="/cubes/new">{m.cubes_new_button()}</Link>
        </Button>
      </div>
      <div className="flex max-w-md flex-col gap-1.5">
        <Label htmlFor="cube-search">{m.cubes_search_label()}</Label>
        <Input
          id="cube-search"
          type="search"
          value={search}
          placeholder={m.cubes_search_placeholder()}
          onChange={(e) => {
            setSearch(e.target.value);
            setPage(0);
          }}
        />
      </div>
      {list.isPending && <p className="text-sm text-fg-muted">{m.loading()}</p>}
      {list.isError && <Alert variant="danger">{list.error.message}</Alert>}
      {list.data && list.data.cubes.length === 0 && (
        <p className="text-sm text-fg-muted">{m.cubes_no_results()}</p>
      )}
      {list.data && list.data.cubes.length > 0 && (
        <ul className="flex flex-col gap-3">
          {list.data.cubes.map((cube) => (
            <li key={cube.id}>
              <CubeListItem cube={cube} />
            </li>
          ))}
        </ul>
      )}
      {totalPages > 1 && (
        <div className="flex items-center gap-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={page === 0}
            onClick={() => setPage((p) => p - 1)}
            aria-label={m.pagination_prev()}
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
            disabled={page + 1 >= totalPages}
            onClick={() => setPage((p) => p + 1)}
            aria-label={m.pagination_next()}
          >
            →
          </Button>
        </div>
      )}
    </div>
  );
}
