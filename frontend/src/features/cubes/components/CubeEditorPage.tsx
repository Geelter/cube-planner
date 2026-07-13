import { getRouteApi, useNavigate } from "@tanstack/react-router";
import { useBlocker } from "@tanstack/react-router";
import { useMemo, useReducer, useState } from "react";
import { m } from "@/paraglide/messages";
import { CardAutocomplete } from "@/shared/cards/CardAutocomplete";
import { Alert } from "@/shared/ui/alert";
import { Label } from "@/shared/ui/label";
import type { CubeCardEntry } from "../api";
import { CommitConflictError, useCommitChange, useCube, useCubeCards } from "../api";
import { emptyPending, pendingCount, pendingReducer, toCommitDiff } from "../lib/pendingDiff";
import type { PendingState } from "../lib/pendingDiff";
import { EditableCardList } from "./EditableCardList";
import { PendingChangesPanel } from "./PendingChangesPanel";

const route = getRouteApi("/cubes/$cubeId/edit");

// Server list + pending deltas = what the user will get after saving.
function previewEntries(server: CubeCardEntry[], pending: PendingState): CubeCardEntry[] {
  const byOracle = new Map(server.map((e) => [e.oracleId, { ...e }]));
  for (const [oracleId, { quantity }] of pending.removes) {
    const entry = byOracle.get(oracleId);
    if (!entry) continue;
    entry.quantity -= quantity;
    if (entry.quantity <= 0) byOracle.delete(oracleId);
  }
  for (const [oracleId, { card, quantity }] of pending.adds) {
    const entry = byOracle.get(oracleId);
    if (entry) {
      entry.quantity += quantity;
    } else {
      byOracle.set(oracleId, {
        scryfallId: card.scryfallId,
        oracleId: card.oracleId,
        name: card.name,
        manaCost: card.manaCost,
        typeLine: card.typeLine,
        cmc: 0, // unknown until saved; grouping puts it in its color bucket fine
        colors: [],
        colorIdentity: [],
        rarity: "",
        imageSmall: card.imageSmall,
        imageNormal: null,
        quantity,
      });
    }
  }
  return [...byOracle.values()];
}

export function CubeEditorPage() {
  const { cubeId } = route.useParams();
  const navigate = useNavigate();
  const cube = useCube(cubeId);
  const cards = useCubeCards(cubeId);
  const commit = useCommitChange(cubeId);
  const [pending, dispatch] = useReducer(pendingReducer, emptyPending);
  const [note, setNote] = useState("");
  const [conflict, setConflict] = useState(false);

  const dirty = pendingCount(pending) > 0;
  useBlocker({
    shouldBlockFn: () => !window.confirm(m.cubes_unsaved_warning()),
    enableBeforeUnload: () => dirty,
    disabled: !dirty,
  });

  const preview = useMemo(
    () => previewEntries(cards.data?.cards ?? [], pending),
    [cards.data, pending],
  );
  // The reducer contract (pendingDiff) wants the unchanged server entry on
  // every dispatch — preview entries have pending deltas baked in, so their
  // quantities would corrupt the remove cap.
  const serverByOracle = useMemo(
    () => new Map((cards.data?.cards ?? []).map((e) => [e.oracleId, e])),
    [cards.data],
  );

  if (cube.isPending || cards.isPending)
    return <p className="text-sm text-fg-muted">{m.loading()}</p>;
  if (cube.isError) return <Alert variant="danger">{cube.error.message}</Alert>;
  if (cards.isError) return <Alert variant="danger">{cards.error.message}</Alert>;

  const save = () => {
    const diff = toCommitDiff(pending);
    setConflict(false);
    commit.mutate(
      { expectedVersion: cards.data.version, note, ...diff },
      {
        onSuccess: () => {
          dispatch({ type: "reset" });
          setNote("");
          // The pending reset only lands on the next render, so the blocker
          // registered on this render still sees dirty state — bypass it.
          void navigate({ to: "/cubes/$cubeId", params: { cubeId }, ignoreBlocker: true });
        },
        onError: async (err) => {
          if (err instanceof CommitConflictError) {
            setConflict(true);
            const fresh = await cards.refetch();
            dispatch({ type: "revalidate", current: fresh.data?.cards ?? [] });
          }
        },
      },
    );
  };

  return (
    <div className="flex flex-col gap-6">
      <h1 className="text-2xl font-semibold text-fg">
        {m.cubes_editor_title({ name: cube.data.name })}
      </h1>

      {conflict && <Alert variant="danger">{m.cubes_conflict_toast()}</Alert>}
      {commit.isError && !(commit.error instanceof CommitConflictError) && (
        <Alert variant="danger">{commit.error.message}</Alert>
      )}

      <div className="flex max-w-md flex-col gap-1.5">
        <Label htmlFor="editor-add">{m.cubes_editor_add_label()}</Label>
        <CardAutocomplete id="editor-add" onSelect={(card) => dispatch({ type: "add", card })} />
      </div>

      <div className="flex flex-col gap-6 lg:flex-row">
        <div className="min-w-0 flex-1">
          <EditableCardList
            cards={preview}
            serverByOracle={serverByOracle}
            groupKind="color"
            dispatch={dispatch}
          />
        </div>
        <PendingChangesPanel
          pending={pending}
          dispatch={dispatch}
          note={note}
          onNoteChange={setNote}
          onSave={save}
          onDiscard={() => {
            dispatch({ type: "reset" });
            setNote("");
          }}
          saving={commit.isPending}
        />
      </div>
    </div>
  );
}
