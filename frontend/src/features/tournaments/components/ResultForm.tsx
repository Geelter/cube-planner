import { useState } from "react";
import { m } from "@/paraglide/messages";
import { Button } from "@/shared/ui/button";
import { Label } from "@/shared/ui/label";
import type { ResultInput, TournamentMatch } from "../api";

function nameOf(players: Map<string, string>, id: string) {
  return players.get(id) ?? "?";
}

const validResult = (r: ResultInput) =>
  r.p1Games >= 0 &&
  r.p1Games <= 2 &&
  r.p2Games >= 0 &&
  r.p2Games <= 2 &&
  r.draws >= 0 &&
  r.draws <= 3 &&
  r.p1Games + r.p2Games + r.draws <= 3 &&
  !(r.p1Games === 2 && r.p2Games === 2);

function GamesField({
  id,
  label,
  value,
  max,
  onChange,
}: {
  id: string;
  label: string;
  value: number;
  max: number;
  onChange: (v: number) => void;
}) {
  return (
    <div className="flex flex-col gap-1">
      <Label htmlFor={id}>{label}</Label>
      <input
        id={id}
        type="number"
        min={0}
        max={max}
        value={value}
        onChange={(e) => onChange(Number(e.target.value))}
        className="w-20 rounded-md border border-border bg-surface px-2 py-1 text-fg"
      />
    </div>
  );
}

export function ResultForm({
  match,
  playerNames,
  onSubmit,
  pending,
  error,
}: {
  match: TournamentMatch;
  playerNames: Map<string, string>;
  onSubmit: (result: ResultInput) => void;
  pending: boolean;
  error: Error | null;
}) {
  const [result, setResult] = useState<ResultInput>({
    p1Games: match.p1Games ?? 0,
    p2Games: match.p2Games ?? 0,
    draws: match.draws ?? 0,
  });
  const [touchedInvalid, setTouchedInvalid] = useState(false);

  return (
    <form
      className="flex flex-wrap items-end gap-3"
      onSubmit={(e) => {
        e.preventDefault();
        if (!validResult(result)) {
          setTouchedInvalid(true);
          return;
        }
        setTouchedInvalid(false);
        onSubmit(result);
      }}
    >
      <GamesField
        id={`p1-${match.id}`}
        label={m.tournament_games_won({ name: nameOf(playerNames, match.player1Id) })}
        value={result.p1Games}
        max={2}
        onChange={(v) => setResult({ ...result, p1Games: v })}
      />
      <GamesField
        id={`p2-${match.id}`}
        label={m.tournament_games_won({
          name: match.player2Id ? nameOf(playerNames, match.player2Id) : "—",
        })}
        value={result.p2Games}
        max={2}
        onChange={(v) => setResult({ ...result, p2Games: v })}
      />
      <GamesField
        id={`draws-${match.id}`}
        label={m.tournament_drawn_games()}
        value={result.draws}
        max={3}
        onChange={(v) => setResult({ ...result, draws: v })}
      />
      <Button type="submit" size="sm" disabled={pending}>
        {m.tournament_report_result()}
      </Button>
      {touchedInvalid && (
        <p role="alert" className="w-full text-sm text-danger">
          {m.tournament_result_invalid()}
        </p>
      )}
      {error && (
        <p role="alert" className="w-full text-sm text-danger">
          {error.message}
        </p>
      )}
    </form>
  );
}
