// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/react";
import { axe } from "vitest-axe";
import { afterEach, expect, test, vi } from "vitest";
import type { TournamentMatch, TournamentStanding } from "../api";
import { ResultForm } from "./ResultForm";
import { StandingsTable } from "./StandingsTable";

afterEach(cleanup);

const match: TournamentMatch = {
  id: "m1",
  tableNumber: 1,
  player1Id: "a",
  player2Id: "b",
};
const names = new Map([
  ["a", "Ann"],
  ["b", "Bob"],
]);
const standings: TournamentStanding[] = [
  {
    rank: 1,
    playerId: "a",
    displayName: "Ann",
    dropped: false,
    matchPoints: 3,
    omwPercent: 50,
    gwPercent: 66.7,
    ogwPercent: 45,
  },
];

test("ResultForm has no axe violations", async () => {
  const { container } = render(
    <ResultForm
      match={match}
      playerNames={names}
      onSubmit={vi.fn()}
      pending={false}
      error={null}
    />,
  );
  expect(await axe(container)).toHaveNoViolations();
});

test("StandingsTable has no axe violations", async () => {
  const { container } = render(<StandingsTable standings={standings} />);
  expect(await axe(container)).toHaveNoViolations();
});
