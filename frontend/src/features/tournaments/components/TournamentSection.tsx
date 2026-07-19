import { useState, type KeyboardEvent } from "react";
import { m } from "@/paraglide/messages";
import { useMe } from "@/features/auth/api";
import { Button } from "@/shared/ui/button";
import { Dialog } from "@/shared/ui/dialog";
import {
  NotFoundError,
  usePlayerAction,
  useReportResult,
  useEventStatus,
  useTournament,
} from "../api";
import type { TournamentMatch } from "../api";
import { ResultForm } from "./ResultForm";
import { StandingsTable } from "./StandingsTable";

const LIVE_POLL_MS = 10_000;

function score(match: TournamentMatch) {
  if (match.reportedAt == null) return m.tournament_playing();
  return `${match.p1Games}–${match.p2Games}${match.draws ? ` (${match.draws})` : ""}`;
}

export function TournamentSection({ eventId }: { eventId: string }) {
  const me = useMe();
  const event = useEventStatus(eventId);
  const live = event.data?.status === "started";
  const relevant = live || event.data?.status === "finished";
  const tournament = useTournament(eventId, {
    refetchInterval: live ? LIVE_POLL_MS : false,
  });
  const report = useReportResult(eventId);
  const playerAction = usePlayerAction(eventId);
  const [tab, setTab] = useState<number | null>(null);
  const [confirmDrop, setConfirmDrop] = useState(false);

  // Not started, no tournament yet, or still loading: render nothing.
  if (!relevant || tournament.isPending || tournament.error instanceof NotFoundError) return null;
  if (tournament.error)
    return (
      <p role="alert" className="text-danger">
        {tournament.error.message}
      </p>
    );

  const t = tournament.data;
  const rounds = (t.rounds ?? []).filter((r) => r.status !== "draft");
  if (rounds.length === 0) return null;
  const activeNumber = tab ?? rounds[rounds.length - 1]!.number;
  // tab is component state and survives eventId changes (the route component
  // is not remounted), so it can point at a round this tournament lacks.
  const round = rounds.find((r) => r.number === activeNumber) ?? rounds[rounds.length - 1]!;
  const players = t.players ?? [];
  const playerNames = new Map(players.map((p) => [p.id, p.displayName]));
  const myPlayer = players.find((p) => p.userId === me.data?.id);
  const matches = round.matches ?? [];
  const myMatch =
    myPlayer && matches.find((mt) => mt.player1Id === myPlayer.id || mt.player2Id === myPlayer.id);
  const canReportMine =
    live &&
    round.status === "published" &&
    myMatch &&
    myMatch.player2Id != null &&
    round.number === rounds[rounds.length - 1]!.number;

  // ARIA tabs keyboard pattern: arrows move focus AND selection, wrapping
  // at the ends; roving tabindex keeps only the selected tab tabbable.
  const onTabKeyDown = (e: KeyboardEvent<HTMLButtonElement>, from: number) => {
    if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
    e.preventDefault();
    const delta = e.key === "ArrowRight" ? 1 : -1;
    const next = (from + delta + rounds.length) % rounds.length;
    setTab(rounds[next]!.number);
    const tabs = e.currentTarget
      .closest('[role="tablist"]')
      ?.querySelectorAll<HTMLButtonElement>('[role="tab"]');
    tabs?.[next]?.focus();
  };

  return (
    <section className="flex flex-col gap-4">
      <h2 className="text-lg font-medium text-fg">{m.tournament_title()}</h2>

      <div role="tablist" className="flex gap-2 overflow-x-auto">
        {rounds.map((r, i) => (
          <button
            key={r.number}
            role="tab"
            aria-selected={r.number === round.number}
            tabIndex={r.number === round.number ? 0 : -1}
            className={`shrink-0 rounded-md border border-border px-3 py-1 text-sm whitespace-nowrap ${
              r.number === round.number ? "bg-accent text-accent-fg" : "text-fg"
            }`}
            onClick={() => setTab(r.number)}
            onKeyDown={(e) => onTabKeyDown(e, i)}
          >
            {m.tournament_round_tab({ number: r.number })}
          </button>
        ))}
      </div>

      <ul className="flex flex-col gap-1">
        {matches.map((mt) => (
          <li
            key={mt.id}
            className={`flex flex-wrap items-center gap-2 rounded-md border border-border p-2 text-sm ${
              myMatch?.id === mt.id ? "bg-surface-raised" : ""
            }`}
          >
            <span className="text-fg-muted">{m.tournament_table({ number: mt.tableNumber })}</span>
            <span className="text-fg">
              {playerNames.get(mt.player1Id)}{" "}
              {mt.player2Id ? (
                <>
                  {m.tournament_vs()} {playerNames.get(mt.player2Id)}
                </>
              ) : (
                <span className="text-fg-muted">— {m.tournament_bye()}</span>
              )}
            </span>
            <span className="ml-auto text-fg-muted">{score(mt)}</span>
          </li>
        ))}
      </ul>

      {canReportMine && myMatch && (
        <div className="rounded-lg border border-border bg-surface-raised p-3">
          <h3 className="mb-2 text-sm font-medium text-fg">{m.tournament_your_match()}</h3>
          <ResultForm
            match={myMatch}
            playerNames={playerNames}
            pending={report.isPending}
            error={report.error}
            onSubmit={(result) => report.mutate({ matchId: myMatch.id, result })}
          />
        </div>
      )}

      <h3 className="text-base font-medium text-fg">{m.tournament_standings()}</h3>
      <StandingsTable standings={t.standings ?? []} highlightPlayerId={myPlayer?.id} />

      {live && myPlayer && !myPlayer.dropped && (
        <div>
          <Button type="button" variant="outline" size="sm" onClick={() => setConfirmDrop(true)}>
            {m.tournament_drop_self()}
          </Button>
        </div>
      )}
      {live && myPlayer?.dropped && (
        <div>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={playerAction.isPending}
            onClick={() => playerAction.mutate({ playerId: myPlayer.id, action: "undrop" })}
          >
            {m.tournament_undrop()}
          </Button>
        </div>
      )}
      {playerAction.error && (
        <p role="alert" className="text-sm text-danger">
          {playerAction.error.message}
        </p>
      )}

      <Dialog
        open={confirmDrop}
        onClose={() => setConfirmDrop(false)}
        title={m.tournament_drop_self()}
      >
        <p className="text-sm text-fg">{m.tournament_drop_confirm()}</p>
        <div className="flex justify-end gap-2">
          <Button type="button" variant="ghost" onClick={() => setConfirmDrop(false)}>
            {m.dialog_close()}
          </Button>
          <Button
            type="button"
            disabled={playerAction.isPending}
            onClick={() => {
              if (myPlayer) playerAction.mutate({ playerId: myPlayer.id, action: "drop" });
              setConfirmDrop(false);
            }}
          >
            {m.tournament_drop()}
          </Button>
        </div>
      </Dialog>
    </section>
  );
}
