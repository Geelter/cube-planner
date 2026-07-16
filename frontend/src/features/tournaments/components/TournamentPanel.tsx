import { useState } from "react";
import { m } from "@/paraglide/messages";
import { useMe } from "@/features/auth/api";
import { Button } from "@/shared/ui/button";
import { Label } from "@/shared/ui/label";
import {
  NotFoundError,
  usePairNextRound,
  usePlayerAction,
  useReportResult,
  useRoundAction,
  useSwapSlots,
  useEventStatus,
  useTournament,
  useUpsertTournament,
} from "../api";
import type { SwapSlotRef, TournamentInfo, TournamentRound } from "../api";
import { ResultForm } from "./ResultForm";
import { StandingsTable } from "./StandingsTable";

function latestRound(t: TournamentInfo): TournamentRound | undefined {
  const rounds = t.rounds ?? [];
  return rounds[rounds.length - 1];
}

export function TournamentPanel({ eventId }: { eventId: string }) {
  const me = useMe();
  const event = useEventStatus(eventId);
  const tournament = useTournament(eventId);
  const upsert = useUpsertTournament(eventId);
  const pair = usePairNextRound(eventId);
  const roundAction = useRoundAction(eventId);
  const swap = useSwapSlots(eventId);
  const report = useReportResult(eventId);
  const playerAction = usePlayerAction(eventId);

  const [plannedRounds, setPlannedRounds] = useState<string>("");
  const [selectedSlot, setSelectedSlot] = useState<SwapSlotRef | null>(null);
  const [editingMatch, setEditingMatch] = useState<string | null>(null);

  if (me.data?.role !== "admin") return null;
  const status = event.data?.status;
  if (status !== "started" && status !== "finished") return null;

  const noTournament = tournament.error instanceof NotFoundError;
  if (tournament.error && !noTournament)
    return (
      <p role="alert" className="text-danger">
        {tournament.error.message}
      </p>
    );
  if (tournament.isPending && !noTournament) return null;

  const t = noTournament ? null : tournament.data!;
  const latest = t ? latestRound(t) : undefined;
  const draft = latest?.status === "draft" ? latest : undefined;
  const published = latest?.status === "published" ? latest : undefined;
  const players = t?.players ?? [];
  const playerNames = new Map(players.map((p) => [p.id, p.displayName]));
  const nextRoundNumber = (latest?.number ?? 0) + 1;
  const canPair =
    status === "started" &&
    !draft &&
    !published &&
    (!t || (t.rounds ?? []).length < t.plannedRounds);
  const draftMatches = draft?.matches ?? [];
  const publishedMatches = published?.matches ?? [];
  const missing = published ? publishedMatches.filter((mt) => mt.reportedAt == null).length : 0;

  const mutationError =
    upsert.error ?? pair.error ?? roundAction.error ?? swap.error ?? playerAction.error;

  const toggleSlot = (ref: SwapSlotRef) => {
    if (!draft) return;
    if (selectedSlot == null) {
      setSelectedSlot(ref);
      return;
    }
    if (selectedSlot.matchId === ref.matchId && selectedSlot.slot === ref.slot) {
      setSelectedSlot(null);
      return;
    }
    swap.mutate({ number: draft.number, a: selectedSlot, b: ref });
    setSelectedSlot(null);
  };

  return (
    <section className="flex flex-col gap-4">
      <h2 className="text-lg font-medium text-fg">{m.tournament_title()}</h2>
      {mutationError && (
        <p role="alert" className="text-sm text-danger">
          {mutationError.message}
        </p>
      )}

      {status === "started" && (
        <form
          className="flex items-end gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            const v = Number(plannedRounds);
            if (Number.isInteger(v) && v >= 1) upsert.mutate(v);
          }}
        >
          <div className="flex flex-col gap-1">
            <Label htmlFor="planned-rounds">{m.tournament_planned_rounds()}</Label>
            <input
              id="planned-rounds"
              type="number"
              min={1}
              max={30}
              value={plannedRounds || (t?.plannedRounds ?? "")}
              onChange={(e) => setPlannedRounds(e.target.value)}
              className="w-24 rounded-md border border-border bg-surface px-2 py-1 text-fg"
            />
          </div>
          <Button type="submit" size="sm" variant="outline" disabled={upsert.isPending}>
            {m.tournament_save()}
          </Button>
          {canPair && (
            <Button
              type="button"
              size="sm"
              disabled={pair.isPending}
              onClick={() => pair.mutate(undefined)}
            >
              {m.tournament_pair_round({ number: nextRoundNumber })}
            </Button>
          )}
        </form>
      )}

      {!t && <p className="text-sm text-fg-muted">{m.tournament_none_yet_organizer()}</p>}

      {draft && (
        <div className="flex flex-col gap-2 rounded-lg border border-border bg-surface-raised p-3">
          <h3 className="text-sm font-medium text-fg">
            {m.tournament_draft_heading({ number: draft.number })}
          </h3>
          <p className="text-xs text-fg-muted">{m.tournament_draft_hint()}</p>
          <ul className="flex flex-col gap-1">
            {draftMatches.map((mt) => (
              <li key={mt.id} className="flex items-center gap-2 text-sm">
                <span className="text-fg-muted">
                  {m.tournament_table({ number: mt.tableNumber })}
                </span>
                {([1, 2] as const).map((slot) => {
                  const playerId = slot === 1 ? mt.player1Id : mt.player2Id;
                  if (playerId == null)
                    return (
                      <span key={slot} className="text-fg-muted">
                        {m.tournament_bye()}
                      </span>
                    );
                  const isSelected = selectedSlot?.matchId === mt.id && selectedSlot?.slot === slot;
                  return (
                    <button
                      key={slot}
                      type="button"
                      aria-pressed={isSelected}
                      className={`rounded-md border px-2 py-0.5 ${
                        isSelected
                          ? "border-accent bg-accent text-accent-fg"
                          : "border-border text-fg"
                      }`}
                      onClick={() => toggleSlot({ matchId: mt.id, slot })}
                    >
                      {playerNames.get(playerId)}
                    </button>
                  );
                })}
              </li>
            ))}
          </ul>
          <div className="flex gap-2">
            <Button
              type="button"
              size="sm"
              variant="outline"
              disabled={roundAction.isPending}
              onClick={() => roundAction.mutate({ action: "reroll", number: draft.number })}
            >
              {m.tournament_reroll()}
            </Button>
            <Button
              type="button"
              size="sm"
              disabled={roundAction.isPending}
              onClick={() => roundAction.mutate({ action: "publish", number: draft.number })}
            >
              {m.tournament_publish()}
            </Button>
          </div>
        </div>
      )}

      {published && (
        <div className="flex flex-col gap-2 rounded-lg border border-border p-3">
          <h3 className="text-sm font-medium text-fg">
            {m.tournament_results_heading({ number: published.number })}
          </h3>
          <ul className="flex flex-col gap-2">
            {publishedMatches.map((mt) => (
              <li key={mt.id} className="flex flex-col gap-1 text-sm">
                <div className="flex items-center gap-2">
                  <span className="text-fg-muted">
                    {m.tournament_table({ number: mt.tableNumber })}
                  </span>
                  <span className="text-fg">
                    {playerNames.get(mt.player1Id)}{" "}
                    {mt.player2Id
                      ? `${m.tournament_vs()} ${playerNames.get(mt.player2Id)}`
                      : `— ${m.tournament_bye()}`}
                  </span>
                  {mt.reportedAt == null ? (
                    <span className="font-medium text-danger">{m.tournament_playing()}</span>
                  ) : (
                    <span className="text-fg-muted">
                      {mt.p1Games}–{mt.p2Games}
                      {mt.draws ? ` (${mt.draws})` : ""}
                    </span>
                  )}
                  {mt.player2Id != null && (
                    <Button
                      type="button"
                      size="sm"
                      variant="ghost"
                      onClick={() => setEditingMatch(editingMatch === mt.id ? null : mt.id)}
                    >
                      {m.tournament_report_result()}
                    </Button>
                  )}
                </div>
                {editingMatch === mt.id && (
                  <ResultForm
                    match={mt}
                    playerNames={playerNames}
                    pending={report.isPending}
                    error={report.error}
                    onSubmit={(result) => {
                      report.mutate(
                        { matchId: mt.id, result },
                        { onSuccess: () => setEditingMatch(null) },
                      );
                    }}
                  />
                )}
              </li>
            ))}
          </ul>
          <div className="flex items-center gap-3">
            <Button
              type="button"
              size="sm"
              disabled={missing > 0 || roundAction.isPending}
              onClick={() => roundAction.mutate({ action: "complete", number: published.number })}
            >
              {m.tournament_complete_round()}
            </Button>
            {missing > 0 && (
              <span className="text-sm text-fg-muted">
                {m.tournament_missing_results({ count: missing })}
              </span>
            )}
          </div>
        </div>
      )}

      {t && (
        <>
          <h3 className="text-base font-medium text-fg">{m.tournament_standings()}</h3>
          <StandingsTable standings={t.standings ?? []} />

          <h3 className="text-base font-medium text-fg">{m.tournament_players_heading()}</h3>
          <ul className="flex flex-col gap-1">
            {players.map((p) => (
              <li key={p.id} className="flex items-center gap-2 text-sm">
                <span className="text-fg">{p.displayName}</span>
                {p.dropped && (
                  <span className="text-xs text-fg-muted">({m.tournament_dropped_flag()})</span>
                )}
                {status === "started" && (
                  <Button
                    type="button"
                    size="sm"
                    variant="ghost"
                    disabled={playerAction.isPending}
                    onClick={() =>
                      playerAction.mutate({
                        playerId: p.id,
                        action: p.dropped ? "undrop" : "drop",
                      })
                    }
                  >
                    {p.dropped ? m.tournament_undrop() : m.tournament_drop()}
                  </Button>
                )}
              </li>
            ))}
          </ul>
        </>
      )}
    </section>
  );
}
