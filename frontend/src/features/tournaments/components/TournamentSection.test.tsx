import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import type { TournamentInfo } from "../api";

const report = vi.fn();
const playerAct = vi.fn();
let tournamentData: TournamentInfo | undefined;
let eventStatus = "started";

vi.mock("@/features/auth/api", () => ({
  useMe: () => ({ data: { id: "u1", role: "user" } }),
}));
vi.mock("../api", async (orig) => ({
  ...(await orig()),
  useEventStatus: () => ({ data: { status: eventStatus } }),
  useTournament: () => ({ data: tournamentData, isPending: false, error: null }),
  useReportResult: () => ({ mutate: report, isPending: false, error: null }),
  usePlayerAction: () => ({ mutate: playerAct, isPending: false, error: null }),
}));

import { TournamentSection } from "./TournamentSection";

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function renderSection() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <TournamentSection eventId="e1" />
    </QueryClientProvider>,
  );
}

function baseTournament(): TournamentInfo {
  return {
    eventId: "e1",
    plannedRounds: 2,
    currentRound: 1,
    players: [
      { id: "pl1", userId: "u1", displayName: "Ann", dropped: false },
      { id: "pl2", userId: "u2", displayName: "Bob", dropped: false },
    ],
    rounds: [
      {
        number: 1,
        status: "published",
        matches: [{ id: "m1", tableNumber: 1, player1Id: "pl1", player2Id: "pl2" }],
      },
    ],
    standings: [
      {
        rank: 1,
        playerId: "pl1",
        displayName: "Ann",
        dropped: false,
        matchPoints: 0,
        omwPercent: 0,
        gwPercent: 0,
        ogwPercent: 0,
      },
      {
        rank: 1,
        playerId: "pl2",
        displayName: "Bob",
        dropped: false,
        matchPoints: 0,
        omwPercent: 0,
        gwPercent: 0,
        ogwPercent: 0,
      },
    ],
  } as TournamentInfo;
}

test("shows pairings, my-match result form, standings, and drop", () => {
  tournamentData = baseTournament();
  renderSection();
  expect(screen.getByRole("tab", { name: "Round 1" })).toBeInTheDocument();
  expect(screen.getByText("Your match")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Report result" })).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Drop from tournament" })).toBeInTheDocument();
});

test("no result form on a completed round; undrop for dropped player", () => {
  tournamentData = baseTournament();
  tournamentData.rounds![0]!.status = "completed";
  tournamentData.players![0]!.dropped = true;
  renderSection();
  expect(screen.queryByRole("button", { name: "Report result" })).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Rejoin" })).toBeInTheDocument();
});

// Keyboard/focus coverage lives in TournamentSection.keyboard.test.tsx (jsdom).

test("stale tab state falls back to exactly one selected tab", async () => {
  tournamentData = baseTournament();
  tournamentData.rounds = [
    {
      number: 1,
      status: "completed",
      matches: [{ id: "m1", tableNumber: 1, player1Id: "pl1", player2Id: "pl2" }],
    },
    {
      number: 2,
      status: "published",
      matches: [{ id: "m2", tableNumber: 1, player1Id: "pl2", player2Id: "pl1" }],
    },
  ];
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const view = render(
    <QueryClientProvider client={qc}>
      <TournamentSection eventId="e1" />
    </QueryClientProvider>,
  );
  await userEvent.click(screen.getByRole("tab", { name: "Round 1" }));

  // The tab state (round 1) survives while the data changes under it —
  // e.g. the route swaps to another event's tournament without remounting.
  tournamentData = baseTournament();
  tournamentData.rounds = [
    {
      number: 2,
      status: "completed",
      matches: [{ id: "m3", tableNumber: 1, player1Id: "pl1", player2Id: "pl2" }],
    },
    {
      number: 3,
      status: "published",
      matches: [{ id: "m4", tableNumber: 1, player1Id: "pl2", player2Id: "pl1" }],
    },
  ];
  view.rerender(
    <QueryClientProvider client={qc}>
      <TournamentSection eventId="e2" />
    </QueryClientProvider>,
  );

  const selected = screen
    .getAllByRole("tab")
    .filter((el) => el.getAttribute("aria-selected") === "true");
  expect(selected).toHaveLength(1);
  expect(selected[0]).toHaveAccessibleName("Round 3");
});

test("renders nothing before the event starts", () => {
  eventStatus = "published";
  tournamentData = baseTournament();
  const { container } = renderSection();
  expect(container).toBeEmptyDOMElement();
  eventStatus = "started";
});
