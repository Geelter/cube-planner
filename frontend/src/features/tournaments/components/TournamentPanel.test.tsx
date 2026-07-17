import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import type { TournamentInfo } from "../api";
import { NotFoundError } from "../api";

const pairMut = vi.fn();
const swapMut = vi.fn();
const roundMut = vi.fn();
let tournamentData: TournamentInfo | null = null;
let tournamentOpts: unknown;

vi.mock("@/features/auth/api", () => ({
  useMe: () => ({ data: { id: "org", role: "admin" } }),
}));
vi.mock("../api", async (orig) => ({
  ...(await orig()),
  useEventStatus: () => ({ data: { status: "started" } }),
  useTournament: (_eventId: string, opts?: unknown) => {
    tournamentOpts = opts;
    return tournamentData
      ? { data: tournamentData, isPending: false, error: null }
      : { data: undefined, isPending: false, error: new NotFoundError("none") };
  },
  useUpsertTournament: () => ({ mutate: vi.fn(), isPending: false, error: null }),
  usePairNextRound: () => ({ mutate: pairMut, isPending: false, error: null }),
  useRoundAction: () => ({ mutate: roundMut, isPending: false, error: null }),
  useSwapSlots: () => ({ mutate: swapMut, isPending: false, error: null }),
  useReportResult: () => ({ mutate: vi.fn(), isPending: false, error: null }),
  usePlayerAction: () => ({ mutate: vi.fn(), isPending: false, error: null }),
}));

import { TournamentPanel } from "./TournamentPanel";

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  tournamentData = null;
  tournamentOpts = undefined;
});

function renderPanel() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <TournamentPanel eventId="e1" />
    </QueryClientProvider>,
  );
}

function draftTournament(): TournamentInfo {
  return {
    eventId: "e1",
    plannedRounds: 2,
    currentRound: 1,
    players: [
      { id: "pl1", userId: "u1", displayName: "Ann", dropped: false },
      { id: "pl2", userId: "u2", displayName: "Bob", dropped: false },
      { id: "pl3", userId: "u3", displayName: "Cid", dropped: false },
      { id: "pl4", userId: "u4", displayName: "Dee", dropped: false },
    ],
    rounds: [
      {
        number: 1,
        status: "draft",
        matches: [
          { id: "m1", tableNumber: 1, player1Id: "pl1", player2Id: "pl2" },
          { id: "m2", tableNumber: 2, player1Id: "pl3", player2Id: "pl4" },
        ],
      },
    ],
    standings: [],
  } as TournamentInfo;
}

test("no tournament yet: shows pair-round-1 CTA", async () => {
  renderPanel();
  const cta = screen.getByRole("button", { name: /pair round 1/i });
  expect(cta).toBeInTheDocument();
  await userEvent.click(cta);
  expect(pairMut).toHaveBeenCalled();
});

test("draft round: select two slots → swap fires", async () => {
  tournamentData = draftTournament();
  renderPanel();
  await userEvent.click(screen.getByRole("button", { name: "Ann" }));
  await userEvent.click(screen.getByRole("button", { name: "Cid" }));
  expect(swapMut).toHaveBeenCalledWith({
    number: 1,
    a: { matchId: "m1", slot: 1 },
    b: { matchId: "m2", slot: 1 },
  });
});

test("published round: complete disabled while results missing", () => {
  tournamentData = draftTournament();
  tournamentData.rounds![0]!.status = "published";
  // One of the two matches reported → one missing.
  tournamentData.rounds![0]!.matches![1] = {
    ...tournamentData.rounds![0]!.matches![1]!,
    p1Games: 2,
    p2Games: 0,
    draws: 0,
    reportedAt: "2026-07-20T18:00:00Z",
  };
  renderPanel();
  expect(screen.getByRole("button", { name: "Complete round" })).toBeDisabled();
  expect(screen.getByText("1 results missing")).toBeInTheDocument();
});

test("publish button fires for a draft round", async () => {
  tournamentData = draftTournament();
  renderPanel();
  await userEvent.click(screen.getByRole("button", { name: "Publish pairings" }));
  expect(roundMut).toHaveBeenCalledWith({ action: "publish", number: 1 });
});

test("polls the tournament while the event is live", () => {
  tournamentData = draftTournament();
  renderPanel();
  expect(tournamentOpts).toEqual({ refetchInterval: 10_000 });
});

test("all planned rounds completed: pair button hidden, add-round hint shown", () => {
  tournamentData = draftTournament();
  tournamentData.plannedRounds = 1;
  tournamentData.rounds![0]!.status = "completed";
  renderPanel();
  expect(screen.queryByRole("button", { name: /pair round/i })).not.toBeInTheDocument();
  expect(screen.getByText(/increase planned rounds/i)).toBeInTheDocument();
});
