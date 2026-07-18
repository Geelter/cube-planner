import { useState } from "react";
import { getLocale } from "@/paraglide/runtime";
import { m } from "@/paraglide/messages";
import { Button } from "@/shared/ui/button";
import { Label } from "@/shared/ui/label";
import type { EventDetail } from "../api";
import { useCubeChangelog, useLinkableCubes, useSetEventCubes } from "../api";

type Draft = { cubeId: string; cubeName: string; cubeChangeId?: string };

export function EventCubesEditor({ event }: { event: EventDetail }) {
  const linkable = useLinkableCubes();
  const setCubes = useSetEventCubes(event.id);
  const [links, setLinks] = useState<Draft[]>(
    (event.cubes ?? []).map((c) => ({
      cubeId: c.cubeId,
      cubeName: c.cubeName,
      ...(c.cubeChangeId ? { cubeChangeId: c.cubeChangeId } : {}),
    })),
  );
  const [adding, setAdding] = useState("");
  const editable = event.status === "draft";

  const save = (next: Draft[]) => {
    const previous = links;
    setLinks(next);
    setCubes.mutate(
      next.map(({ cubeId, cubeChangeId }) => ({
        cubeId,
        ...(cubeChangeId ? { cubeChangeId } : {}),
      })),
      { onError: () => setLinks(previous) },
    );
  };

  return (
    <section className="flex flex-col gap-3">
      <h2 className="text-lg font-medium text-fg">{m.event_cubes_editor_title()}</h2>
      {setCubes.error && (
        <p role="alert" className="text-sm text-danger">
          {setCubes.error.message}
        </p>
      )}
      <ul className="flex flex-col gap-2">
        {links.map((l) => (
          <li key={l.cubeId} className="flex flex-wrap items-center gap-2 text-sm">
            <span className="font-medium text-fg">{l.cubeName}</span>
            {editable && (
              <>
                <PinPicker
                  link={l}
                  onChange={(cubeChangeId) =>
                    save(
                      links.map((x) =>
                        x.cubeId === l.cubeId
                          ? { ...x, ...(cubeChangeId ? { cubeChangeId } : {}) }
                          : x,
                      ),
                    )
                  }
                />
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => save(links.filter((x) => x.cubeId !== l.cubeId))}
                >
                  {m.event_cubes_remove()}
                </Button>
              </>
            )}
          </li>
        ))}
      </ul>
      {editable && (
        <div className="flex items-end gap-2">
          <div className="flex flex-col gap-1">
            <Label htmlFor="cube-add">{m.event_cubes_add()}</Label>
            <select
              id="cube-add"
              className="rounded-md border border-border bg-surface px-3 py-2 text-fg"
              value={adding}
              onChange={(e) => setAdding(e.target.value)}
            >
              <option value="">{m.event_cubes_add_placeholder()}</option>
              {(linkable.data ?? [])
                .filter((c) => !links.some((l) => l.cubeId === c.id))
                .map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name}
                  </option>
                ))}
            </select>
          </div>
          <Button
            type="button"
            size="sm"
            disabled={!adding}
            onClick={() => {
              const cube = linkable.data?.find((c) => c.id === adding);
              if (!cube) return;
              save([...links, { cubeId: cube.id, cubeName: cube.name }]);
              setAdding("");
            }}
          >
            {m.event_cubes_add()}
          </Button>
        </div>
      )}
    </section>
  );
}

function PinPicker({ link, onChange }: { link: Draft; onChange: (changeId?: string) => void }) {
  const changes = useCubeChangelog(link.cubeId);
  const id = `pin-${link.cubeId}`;
  return (
    <span className="flex items-center gap-1">
      <Label htmlFor={id} className="text-xs text-fg-muted">
        {m.event_cubes_pin_label()}
      </Label>
      <select
        id={id}
        className="rounded-md border border-border bg-surface px-2 py-1 text-xs text-fg"
        value={link.cubeChangeId ?? ""}
        onChange={(e) => onChange(e.target.value || undefined)}
      >
        <option value="">{m.event_cubes_pin_live()}</option>
        {(changes.data ?? []).map((ch) => (
          <option key={ch.id} value={ch.id}>
            v{ch.version} · {new Date(ch.createdAt).toLocaleDateString(getLocale())}
          </option>
        ))}
      </select>
    </span>
  );
}
