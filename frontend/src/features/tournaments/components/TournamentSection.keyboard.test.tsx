// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import type { TournamentInfo } from "../api";

const report = vi.fn();
const playerAct = vi.fn();
let tournamentData: TournamentInfo | undefined;

vi.mock("@/features/auth/api", () => ({
  useMe: () => ({ data: { id: "u1", role: "user" } }),
}));
vi.mock("../api", async (orig) => ({
  ...(await orig()),
  useEventStatus: () => ({ data: { status: "started" } }),
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

function twoRoundTournament(): TournamentInfo {
  return {
    eventId: "e1",
    plannedRounds: 2,
    currentRound: 2,
    players: [
      { id: "pl1", userId: "u1", displayName: "Ann", dropped: false },
      { id: "pl2", userId: "u2", displayName: "Bob", dropped: false },
    ],
    rounds: [
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
    ],
    standings: [],
  } as TournamentInfo;
}

test("arrow keys move focus and selection across round tabs (roving tabindex)", async () => {
  tournamentData = twoRoundTournament();
  renderSection();
  const tab1 = screen.getByRole("tab", { name: "Round 1" });
  const tab2 = screen.getByRole("tab", { name: "Round 2" });
  // Latest round is selected by default; only the selected tab is tabbable.
  expect(tab2).toHaveAttribute("aria-selected", "true");
  expect(tab2).toHaveAttribute("tabindex", "0");
  expect(tab1).toHaveAttribute("tabindex", "-1");

  tab2.focus();
  await userEvent.keyboard("{ArrowLeft}");
  expect(tab1).toHaveFocus();
  expect(tab1).toHaveAttribute("aria-selected", "true");
  expect(tab2).toHaveAttribute("aria-selected", "false");

  await userEvent.keyboard("{ArrowRight}");
  expect(tab2).toHaveFocus();
  expect(tab2).toHaveAttribute("aria-selected", "true");

  // Wraps at the ends.
  await userEvent.keyboard("{ArrowRight}");
  expect(tab1).toHaveFocus();
  expect(tab1).toHaveAttribute("aria-selected", "true");
});
