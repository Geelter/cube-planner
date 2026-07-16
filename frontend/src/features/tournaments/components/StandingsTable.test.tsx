import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, test } from "vitest";
import type { TournamentStanding } from "../api";
import { StandingsTable } from "./StandingsTable";

afterEach(cleanup);

const rows: TournamentStanding[] = [
  {
    rank: 1,
    playerId: "a",
    displayName: "Ann",
    dropped: false,
    matchPoints: 6,
    omwPercent: 50,
    gwPercent: 66.7,
    ogwPercent: 45,
  },
  {
    rank: 2,
    playerId: "b",
    displayName: "Bob",
    dropped: true,
    matchPoints: 3,
    omwPercent: 66.7,
    gwPercent: 50,
    ogwPercent: 55,
  },
];

test("renders ranks, points, percentages, and the dropped flag", () => {
  render(<StandingsTable standings={rows} highlightPlayerId="a" />);
  const [, first, second] = screen.getAllByRole("row");
  expect(first).toHaveTextContent("Ann");
  expect(first).toHaveTextContent("6");
  expect(first).toHaveTextContent("66.7");
  expect(second).toHaveTextContent("(dropped)");
});
