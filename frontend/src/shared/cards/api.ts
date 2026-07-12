import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { m } from "@/paraglide/messages";
import { client } from "@/shared/api/client";
import type { components } from "@/shared/api/schema";

export type CardSummary = components["schemas"]["CardSummary"];

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
      if (error) throw new Error(error.detail ?? m.error_generic());
      if (!data) throw new Error(m.error_generic());
      return data.cards ?? [];
    },
  });
}
