import { m } from "@/paraglide/messages";
import type { TournamentStanding } from "../api";

const pct = (v: number) => v.toFixed(1);

export function StandingsTable({
  standings,
  highlightPlayerId,
}: {
  standings: TournamentStanding[];
  highlightPlayerId?: string | undefined;
}) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-md text-sm">
        <caption className="sr-only">{m.tournament_standings()}</caption>
        <thead>
          <tr className="border-b border-border text-left text-fg-muted">
            <th scope="col" className="py-1 pr-2">
              {m.tournament_rank()}
            </th>
            <th scope="col" className="py-1 pr-2">
              {m.tournament_player()}
            </th>
            <th scope="col" className="py-1 pr-2 text-right">
              {m.tournament_points()}
            </th>
            <th scope="col" className="py-1 pr-2 text-right">
              {m.tournament_omw()}
            </th>
            <th scope="col" className="py-1 pr-2 text-right">
              {m.tournament_gw()}
            </th>
            <th scope="col" className="py-1 text-right">
              {m.tournament_ogw()}
            </th>
          </tr>
        </thead>
        <tbody>
          {standings.map((s) => (
            <tr
              key={s.playerId}
              className={`border-b border-border ${
                s.playerId === highlightPlayerId ? "bg-surface-raised font-medium" : ""
              }`}
            >
              <td className="py-1 pr-2">{s.rank}</td>
              <td className="py-1 pr-2 text-fg">
                {s.displayName}
                {s.dropped && (
                  <span className="ml-2 text-xs text-fg-muted">
                    ({m.tournament_dropped_flag()})
                  </span>
                )}
              </td>
              <td className="py-1 pr-2 text-right">{s.matchPoints}</td>
              <td className="py-1 pr-2 text-right">{pct(s.omwPercent)}</td>
              <td className="py-1 pr-2 text-right">{pct(s.gwPercent)}</td>
              <td className="py-1 text-right">{pct(s.ogwPercent)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
