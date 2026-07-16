import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { m } from "@/paraglide/messages";
import { client } from "@/shared/api/client";
import type { components } from "@/shared/api/schema";

export type TournamentInfo = components["schemas"]["TournamentInfo"];
export type TournamentRound = components["schemas"]["TournamentRoundInfo"];
export type TournamentMatch = components["schemas"]["TournamentMatchInfo"];
export type TournamentPlayer = components["schemas"]["TournamentPlayerInfo"];
export type TournamentStanding = components["schemas"]["TournamentStandingInfo"];

/** 404 = no tournament yet — a normal state, not an error banner. */
export class NotFoundError extends Error {}

function unwrap<T>(data: T | undefined, error: { detail?: string | null } | undefined): T {
  if (error) throw new Error(error.detail ?? m.error_generic());
  if (!data) throw new Error(m.error_generic());
  return data;
}

// The event detail under the events feature's queryKey: same endpoint +
// key, so TanStack dedupes with features/events and their invalidations
// keep both fresh. features must not import features (structure.md).
export function useEventStatus(eventId: string) {
  return useQuery({
    queryKey: ["events", "detail", eventId],
    retry: false,
    queryFn: async () => {
      const { data, error } = await client.GET("/api/events/{eventId}", {
        params: { path: { eventId } },
      });
      return unwrap(data, error);
    },
  });
}

export function useTournament(eventId: string, opts?: { refetchInterval?: number | false }) {
  return useQuery({
    queryKey: ["tournaments", eventId],
    retry: false,
    refetchInterval: opts?.refetchInterval ?? false,
    queryFn: async () => {
      const { data, error, response } = await client.GET("/api/events/{eventId}/tournament", {
        params: { path: { eventId } },
      });
      if (response.status === 404) throw new NotFoundError(m.tournament_none_yet());
      return unwrap(data, error);
    },
  });
}

// Server truth only (no optimistic updates): every mutation refetches
// the aggregate.
function useTournamentMutation<TVars>(eventId: string, fn: (vars: TVars) => Promise<unknown>) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["tournaments", eventId] }),
  });
}

export function useUpsertTournament(eventId: string) {
  return useTournamentMutation(eventId, async (plannedRounds: number | undefined) => {
    const { data, error } = await client.PUT("/api/events/{eventId}/tournament", {
      params: { path: { eventId } },
      body: plannedRounds == null ? {} : { plannedRounds },
    });
    return unwrap(data, error);
  });
}

export function usePairNextRound(eventId: string) {
  return useTournamentMutation(eventId, async () => {
    const { data, error } = await client.POST("/api/events/{eventId}/tournament/rounds", {
      params: { path: { eventId } },
    });
    return unwrap(data, error);
  });
}

export type RoundAction = "reroll" | "publish" | "complete";

const ROUND_PATHS = {
  reroll: "/api/events/{eventId}/tournament/rounds/{number}/reroll",
  publish: "/api/events/{eventId}/tournament/rounds/{number}/publish",
  complete: "/api/events/{eventId}/tournament/rounds/{number}/complete",
} as const;

export function useRoundAction(eventId: string) {
  return useTournamentMutation(
    eventId,
    async ({ action, number }: { action: RoundAction; number: number }) => {
      const { data, error } = await client.POST(ROUND_PATHS[action], {
        params: { path: { eventId, number } },
      });
      return unwrap(data, error);
    },
  );
}

export type SwapSlotRef = { matchId: string; slot: 1 | 2 };

export function useSwapSlots(eventId: string) {
  return useTournamentMutation(
    eventId,
    async ({ number, a, b }: { number: number; a: SwapSlotRef; b: SwapSlotRef }) => {
      const { data, error } = await client.POST(
        "/api/events/{eventId}/tournament/rounds/{number}/swap",
        { params: { path: { eventId, number } }, body: { a, b } },
      );
      return unwrap(data, error);
    },
  );
}

export type ResultInput = { p1Games: number; p2Games: number; draws: number };

export function useReportResult(eventId: string) {
  return useTournamentMutation(
    eventId,
    async ({ matchId, result }: { matchId: string; result: ResultInput }) => {
      const { data, error } = await client.PUT(
        "/api/events/{eventId}/tournament/matches/{matchId}/result",
        { params: { path: { eventId, matchId } }, body: result },
      );
      return unwrap(data, error);
    },
  );
}

export function usePlayerAction(eventId: string) {
  return useTournamentMutation(
    eventId,
    async ({ playerId, action }: { playerId: string; action: "drop" | "undrop" }) => {
      const path =
        action === "drop"
          ? ("/api/events/{eventId}/tournament/players/{playerId}/drop" as const)
          : ("/api/events/{eventId}/tournament/players/{playerId}/undrop" as const);
      const { data, error } = await client.POST(path, {
        params: { path: { eventId, playerId } },
      });
      return unwrap(data, error);
    },
  );
}
