import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { m } from "@/paraglide/messages";
import { client } from "@/shared/api/client";
import { unwrap } from "@/shared/api/helpers";
import type { components } from "@/shared/api/schema";

export type CardSummary = components["schemas"]["CardSummary"];
export type CardDetail = components["schemas"]["CardDetail"];

export function useCardAutocomplete(q: string) {
  const query = q.trim();
  return useQuery({
    queryKey: ["cards", "autocomplete", query],
    enabled: query.length >= 2,
    // Keep showing the previous list while the next keystroke's results
    // load — prevents the listbox flickering closed on every keypress.
    placeholderData: keepPreviousData,
    queryFn: async (): Promise<CardSummary[]> => {
      const { data, error } = await client.GET("/api/cards/autocomplete", {
        params: { query: { q: query } },
      });
      return unwrap(data, error).cards ?? [];
    },
  });
}

export function useCardPrintings(oracleId: string | undefined) {
  return useQuery({
    queryKey: ["cards", "printings", oracleId],
    enabled: oracleId !== undefined,
    queryFn: async (): Promise<CardDetail[]> => {
      if (oracleId === undefined) throw new Error(m.error_generic());
      const { data, error } = await client.GET("/api/cards/{oracleId}/printings", {
        params: { path: { oracleId } },
      });
      return unwrap(data, error).printings ?? [];
    },
  });
}
