import { Link, getRouteApi } from "@tanstack/react-router";
import { useState } from "react";
import { m } from "@/paraglide/messages";
import { Alert } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { useCube, useCubeCards } from "../api";
import type { GroupKind } from "../lib/grouping";
import { GroupedCardList } from "./GroupedCardList";

const route = getRouteApi("/cubes/$cubeId/");

const GROUP_KINDS: { kind: GroupKind; label: () => string }[] = [
  { kind: "color", label: () => m.cubes_group_color() },
  { kind: "type", label: () => m.cubes_group_type() },
  { kind: "cmc", label: () => m.cubes_group_cmc() },
];

export function CubeDisplayPage() {
  const { cubeId } = route.useParams();
  const { atVersion } = route.useSearch();
  const [groupKind, setGroupKind] = useState<GroupKind>("color");
  const cube = useCube(cubeId);
  const cards = useCubeCards(cubeId, atVersion);

  if (cube.isPending) return <p className="text-sm text-fg-muted">{m.loading()}</p>;
  if (cube.isError) return <Alert variant="danger">{cube.error.message}</Alert>;

  const viewingPast = atVersion !== undefined && atVersion !== cube.data.version;

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold text-fg">
            {cube.data.name}{" "}
            <span className="text-base font-normal text-fg-muted">
              {m.cubes_version_badge({ version: cards.data?.version ?? cube.data.version })}
            </span>
          </h1>
          <p className="text-sm text-fg-muted">
            {m.cubes_by_owner({ owner: cube.data.ownerName })} ·{" "}
            {m.cubes_card_count({ count: cube.data.cardCount })}
            {cube.data.visibility === "private" && <> · {m.cubes_visibility_private()}</>}
          </p>
          {cube.data.description !== "" && (
            <p className="mt-2 max-w-prose text-sm text-fg">{cube.data.description}</p>
          )}
        </div>
        <div className="flex gap-2">
          <Button asChild variant="outline" size="sm">
            <Link to="/cubes/$cubeId/history" params={{ cubeId }}>
              {m.cubes_history_button()}
            </Link>
          </Button>
          {!viewingPast && (
            <Button asChild size="sm">
              <Link to="/cubes/$cubeId/edit" params={{ cubeId }}>
                {m.cubes_edit_button()}
              </Link>
            </Button>
          )}
        </div>
      </div>

      {viewingPast && (
        <Alert variant="default">
          {m.cubes_viewing_version({ version: atVersion })}{" "}
          <Link
            to="/cubes/$cubeId"
            params={{ cubeId }}
            search={{}}
            className="font-medium underline"
          >
            {m.cubes_back_to_current()}
          </Link>
        </Alert>
      )}

      <fieldset className="flex items-center gap-2" aria-label={m.cubes_group_by()}>
        <legend className="text-sm text-fg-muted">{m.cubes_group_by()}</legend>
        {GROUP_KINDS.map(({ kind, label }) => (
          <Button
            key={kind}
            type="button"
            size="sm"
            variant={groupKind === kind ? "default" : "outline"}
            aria-pressed={groupKind === kind}
            onClick={() => setGroupKind(kind)}
          >
            {label()}
          </Button>
        ))}
      </fieldset>

      {cards.isPending && <p className="text-sm text-fg-muted">{m.loading()}</p>}
      {cards.isError && <Alert variant="danger">{cards.error.message}</Alert>}
      {cards.data && <GroupedCardList cards={cards.data.cards} groupKind={groupKind} />}
    </div>
  );
}
