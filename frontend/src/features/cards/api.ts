import { useQuery } from "@tanstack/react-query";
import { m } from "@/paraglide/messages";
import { client } from "@/shared/api/client";
import type { components } from "@/shared/api/schema";

export type CardDetail = components["schemas"]["CardDetail"];

export function useCardPrintings(oracleId: string | undefined) {
  return useQuery({
    queryKey: ["cards", "printings", oracleId],
    enabled: oracleId !== undefined,
    queryFn: async (): Promise<CardDetail[]> => {
      if (oracleId === undefined) throw new Error(m.error_generic());
      const { data, error } = await client.GET("/api/cards/{oracleId}/printings", {
        params: { path: { oracleId } },
      });
      if (error) throw new Error(error.detail ?? m.error_generic());
      if (!data) throw new Error(m.error_generic());
      return data.printings ?? [];
    },
  });
}
