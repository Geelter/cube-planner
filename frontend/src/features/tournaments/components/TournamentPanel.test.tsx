import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import { m } from "@/paraglide/messages";
import type { TournamentInfo, TournamentMatch } from "../api";

const pairMut = vi.fn();
const swapMut = vi.fn();
const roundMut = vi.fn();
const upsertMut = vi.fn();
// undefined = still loading (no aggregate yet, e.g. before the first fetch
// resolves); every other case is a full TournamentInfo, exists true or false.
let tournamentData: TournamentInfo | undefined;
let tournamentError: Error | null = null;
let tournamentOpts: unknown;
const roundState: { isPending: boolean; variables?: { action: string; number: number } } = {
  isPending: false,
};

vi.mock("@/features/auth/api", () => ({
  useMe: () => ({ data: { id: "org", role: "admin" } }),
}));
vi.mock("../api", async (orig) => ({
  ...(await orig()),
  useEventStatus: () => ({ data: { status: "started" } }),
  useTournament: (_eventId: string, opts?: unknown) => {
    tournamentOpts = opts;
    return { data: tournamentData, isPending: false, error: tournamentError };
  },
  useUpsertTournament: () => ({ mutate: upsertMut, isPending: false, error: null }),
  usePairNextRound: () => ({ mutate: pairMut, isPending: false, error: null }),
  useRoundAction: () => ({ mutate: roundMut, error: null, ...roundState }),
  useSwapSlots: () => ({ mutate: swapMut, isPending: false, error: null }),
  useReportResult: () => ({ mutate: vi.fn(), isPending: false, error: null }),
  usePlayerAction: () => ({ mutate: vi.fn(), isPending: false, error: null }),
}));

import { TournamentPanel } from "./TournamentPanel";

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  tournamentData = undefined;
  tournamentError = null;
  tournamentOpts = undefined;
  roundState.isPending = false;
  delete roundState.variables;
});

function renderPanel() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  // Fresh element per (re)render — reusing one element reference makes React
  // bail out of reconciling the subtree, hiding mock-state changes.
  const makeUi = () => (
    <QueryClientProvider client={qc}>
      <TournamentPanel eventId="e1" />
    </QueryClientProvider>
  );
  const view = render(makeUi());
  return { ...view, rerenderSame: () => view.rerender(makeUi()) };
}

/** The aggregate shape Get returns before the organizer creates a tournament. */
function noTournamentYet(overrides: Partial<TournamentInfo> = {}): TournamentInfo {
  return {
    eventId: "e1",
    exists: false,
    plannedRounds: 0,
    recommendedRounds: 1,
    paidPlayerCount: 0,
    players: [],
    rounds: [],
    standings: [],
    ...overrides,
  } as TournamentInfo;
}

function draftTournament(): TournamentInfo {
  return {
    eventId: "e1",
    exists: true,
    plannedRounds: 2,
    recommendedRounds: 2,
    paidPlayerCount: 4,
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
          matchDefaults({ id: "m1", tableNumber: 1, player1Id: "pl1", player2Id: "pl2" }),
          matchDefaults({ id: "m2", tableNumber: 2, player1Id: "pl3", player2Id: "pl4" }),
        ],
      },
    ],
    standings: [],
  } as TournamentInfo;
}

/** Fills in the dispute/report fields every match now carries on the wire. */
function matchDefaults(overrides: Partial<TournamentMatch>): TournamentMatch {
  return {
    id: "m",
    tableNumber: 1,
    player1Id: "pl1",
    disputed: false,
    hadDispute: false,
    reports: [],
    resultLocked: false,
    ...overrides,
  };
}

/** A tournament with one published round holding exactly the given matches. */
function publishedTournament(matches: Partial<TournamentMatch>[]): TournamentInfo {
  return {
    ...draftTournament(),
    rounds: [
      {
        number: 1,
        status: "published",
        matches: matches.map((mt) => matchDefaults({ player2Id: "pl2", ...mt })),
      },
    ],
  } as TournamentInfo;
}

function renderPanelWithMatch(match: Partial<TournamentMatch>) {
  tournamentData = publishedTournament([match]);
  renderPanel();
}

function renderPanelWithTwoMatches() {
  tournamentData = publishedTournament([
    { id: "m1", tableNumber: 1, player1Id: "pl1", player2Id: "pl2" },
    { id: "m2", tableNumber: 2, player1Id: "pl3", player2Id: "pl4" },
  ]);
  renderPanel();
}

test("no tournament yet: shows pair-round-1 CTA", async () => {
  tournamentData = noTournamentYet();
  renderPanel();
  const cta = screen.getByRole("button", { name: /pair round 1/i });
  expect(cta).toBeInTheDocument();
  await userEvent.click(cta);
  expect(pairMut).toHaveBeenCalled();
});

test("prefills the planned-rounds input from the server recommendation", async () => {
  tournamentData = noTournamentYet({ recommendedRounds: 4, paidPlayerCount: 12 });
  renderPanel();
  const input = await screen.findByLabelText(/planned rounds/i);
  expect(input).toHaveValue(4);
});

test("prefers the stored plannedRounds once a tournament exists", async () => {
  tournamentData = {
    ...noTournamentYet({ recommendedRounds: 4, paidPlayerCount: 12 }),
    exists: true,
    plannedRounds: 5,
  };
  renderPanel();
  const input = await screen.findByLabelText(/planned rounds/i);
  expect(input).toHaveValue(5);
});

test("keeps the section rendered while a background refetch is in flight", async () => {
  tournamentData = draftTournament();
  const { rerenderSame } = renderPanel();
  expect(
    await screen.findByRole("heading", { name: m.tournament_standings() }),
  ).toBeInTheDocument();
  // A single failed 10s poll: keepPreviousData means `data` stays the last
  // good aggregate even though the hook now also carries an error.
  tournamentError = new Error("network error");
  rerenderSame();
  expect(screen.getByRole("heading", { name: m.tournament_standings() })).toBeInTheDocument();
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
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
  expect(screen.getByText("1 result missing")).toBeInTheDocument();
});

test("reroll clears a pending slot selection", async () => {
  tournamentData = draftTournament();
  renderPanel();
  const ann = screen.getByRole("button", { name: "Ann" });
  await userEvent.click(ann);
  expect(ann).toHaveAttribute("aria-pressed", "true");
  await userEvent.click(screen.getByRole("button", { name: "Re-roll" }));
  // New pairings invalidate the stored match/slot ref; keeping it selected
  // makes the next click swap against a match that no longer exists (422).
  expect(ann).toHaveAttribute("aria-pressed", "false");
  expect(roundMut).toHaveBeenCalledWith({ action: "reroll", number: 1 });
});

test("publish button fires for a draft round", async () => {
  tournamentData = draftTournament();
  renderPanel();
  await userEvent.click(screen.getByRole("button", { name: "Publish pairings" }));
  expect(roundMut).toHaveBeenCalledWith({ action: "publish", number: 1 });
});

test("publish spins while reroll is only disabled", () => {
  tournamentData = draftTournament();
  roundState.isPending = true;
  roundState.variables = { action: "publish", number: 1 };
  renderPanel();
  // Match by textContent, not accessible name: the spinner's visually-hidden
  // "Loading…" label joins the accessible name while a button is busy.
  const publish = screen
    .getAllByRole("button")
    .find((b) => b.textContent === m.tournament_publish())!;
  const reroll = screen
    .getAllByRole("button")
    .find((b) => b.textContent === m.tournament_reroll())!;
  expect(publish.getAttribute("aria-busy")).toBe("true");
  expect(reroll.getAttribute("aria-busy")).not.toBe("true");
  expect(reroll).toBeDisabled();
});

test("polls the tournament while the event is live", () => {
  tournamentData = draftTournament();
  renderPanel();
  expect(tournamentOpts).toEqual({ refetchInterval: 10_000 });
});

test("planned-rounds input can be cleared; empty value is not submitted", async () => {
  tournamentData = draftTournament();
  renderPanel();
  const input = screen.getByLabelText("Planned rounds");
  expect(input).toHaveValue(2);
  await userEvent.clear(input);
  // Clearing must stick — the field may not snap back to the server value.
  expect(input).toHaveValue(null);
  await userEvent.click(screen.getByRole("button", { name: "Save" }));
  expect(upsertMut).not.toHaveBeenCalled();
  await userEvent.type(input, "5");
  await userEvent.click(screen.getByRole("button", { name: "Save" }));
  expect(upsertMut).toHaveBeenCalledWith(5);
});

test("all planned rounds completed: pair button hidden, add-round hint shown", () => {
  tournamentData = draftTournament();
  tournamentData.plannedRounds = 1;
  tournamentData.rounds![0]!.status = "completed";
  renderPanel();
  expect(screen.queryByRole("button", { name: /pair round/i })).not.toBeInTheDocument();
  expect(screen.getByText(/increase planned rounds/i)).toBeInTheDocument();
});

test("shows a disputed badge and the report history to the organizer", async () => {
  renderPanelWithMatch({
    id: "m1",
    tableNumber: 3,
    disputed: true,
    hadDispute: true,
    reports: [
      {
        reporterName: "Ann",
        isOrganizer: false,
        p1Games: 2,
        p2Games: 1,
        reportedAt: "2026-09-03T14:02:00Z",
      },
      {
        reporterName: "Bob",
        isOrganizer: false,
        p1Games: 1,
        p2Games: 2,
        reportedAt: "2026-09-03T14:05:00Z",
      },
    ],
  });
  expect(await screen.findByText("Disputed")).toBeInTheDocument();
  expect(screen.getByText(/Ann reported 2–1/)).toBeInTheDocument();
  expect(screen.getByText(/Bob reported 1–2/)).toBeInTheDocument();
});

test("a resolved-after-disagreement match shows the quiet hadDispute marker, not the badge", async () => {
  renderPanelWithMatch({ id: "m1", disputed: false, hadDispute: true });
  expect(await screen.findByText("Resolved after disagreement")).toBeInTheDocument();
  expect(screen.queryByText("Disputed")).not.toBeInTheDocument();
});

test("confirms before discarding an unsaved result when opening another table", async () => {
  renderPanelWithTwoMatches();
  // Capture the two row toggles up front — once the first form opens, its
  // own submit button also reads "Report result" and would otherwise be
  // picked up by a fresh query.
  const toggles = screen.getAllByRole("button", { name: "Report result" });
  await userEvent.click(toggles[0]!);
  await userEvent.clear(screen.getByLabelText("Ann: games won"));
  await userEvent.type(screen.getByLabelText("Ann: games won"), "2");
  await userEvent.click(toggles[1]!);

  expect(screen.getByText(/table 1.*has not been submitted/i)).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Discard" }));
  // The second table's form is now the open one, showing its own pristine
  // values — the first table's edited form is gone entirely.
  expect(screen.queryByLabelText("Ann: games won")).not.toBeInTheDocument();
  expect(screen.getByLabelText("Cid: games won")).toHaveValue(0);
});

test("opening another table without edits does not prompt", async () => {
  renderPanelWithTwoMatches();
  const toggles = screen.getAllByRole("button", { name: "Report result" });
  await userEvent.click(toggles[0]!);
  await userEvent.click(toggles[1]!);
  expect(screen.queryByText(/has not been submitted/i)).not.toBeInTheDocument();
  // The second table's form is the one now open.
  expect(screen.getByLabelText("Cid: games won")).toBeInTheDocument();
});
