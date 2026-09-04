import { useMutation } from "@tanstack/react-query";
import { client } from "@/shared/api/client";
import { unwrap } from "@/shared/api/helpers";
import type { components } from "@/shared/api/schema";

export type ImportCardMatch = components["schemas"]["ImportCardMatch"];
export type ImportResolveLine = components["schemas"]["ImportResolveLine"];

/** Resolves a pasted card list against the shared cards resolver. Used by
 *  both the collection importer and the cube import flow (Task 19). */
export function useResolveCardList() {
  return useMutation({
    mutationFn: async (vars: { text: string }): Promise<ImportResolveLine[]> => {
      const { data, error } = await client.POST("/api/cards/resolve-list", {
        body: { text: vars.text },
      });
      return unwrap(data, error).lines ?? [];
    },
  });
}
