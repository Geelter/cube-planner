import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { m } from "@/paraglide/messages";
import { client } from "@/shared/api/client";
import type { components } from "@/shared/api/schema";

export type CollectionItemEntry = components["schemas"]["CollectionItemEntry"];
export type ImportCardMatch = components["schemas"]["ImportCardMatch"];
export type ImportResolveLine = components["schemas"]["ImportResolveLine"];
export type WantlistEntry = components["schemas"]["WantlistEntry"];

export const COLLECTION_PAGE_SIZE = 50;

/** Thrown on 401 so pages can render a login prompt instead of a generic error. */
export class UnauthorizedError extends Error {}

export function useCollection(search: string, page: number) {
  const query = search.trim();
  return useQuery({
    queryKey: ["collection", "list", query, page],
    placeholderData: keepPreviousData,
    retry: false,
    queryFn: async () => {
      const { data, error, response } = await client.GET("/api/collection", {
        params: {
          query: {
            search: query,
            limit: COLLECTION_PAGE_SIZE,
            offset: page * COLLECTION_PAGE_SIZE,
          },
        },
      });
      if (response.status === 401) throw new UnauthorizedError(m.collection_login_required());
      if (error) throw new Error(error.detail ?? m.error_generic());
      if (!data) throw new Error(m.error_generic());
      return { items: data.items ?? [], total: data.total, totalCopies: data.totalCopies };
    },
  });
}

export function useSetQuantity() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { scryfallId: string; quantity: number }) => {
      const { data, error } = await client.PUT("/api/collection/cards/{scryfallId}", {
        params: { path: { scryfallId: vars.scryfallId } },
        body: { quantity: vars.quantity },
      });
      if (error) throw new Error(error.detail ?? m.error_generic());
      return data?.item ?? null;
    },
    // Settled, not success: after a failure the list must resync, or the
    // debounced stepper keeps showing a value the server rejected.
    onSettled: () => qc.invalidateQueries({ queryKey: ["collection"] }),
  });
}

export function useChangePrinting() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { scryfallId: string; newScryfallId: string }) => {
      const { data, error } = await client.POST(
        "/api/collection/cards/{scryfallId}/change-printing",
        {
          params: { path: { scryfallId: vars.scryfallId } },
          body: { newScryfallId: vars.newScryfallId },
        },
      );
      if (error) throw new Error(error.detail ?? m.error_generic());
      return data?.item ?? null;
    },
    onSettled: () => qc.invalidateQueries({ queryKey: ["collection"] }),
  });
}

export function useResolveImport() {
  return useMutation({
    mutationFn: async (vars: { text: string }): Promise<ImportResolveLine[]> => {
      const { data, error } = await client.POST("/api/collection/import/resolve", {
        body: { text: vars.text },
      });
      if (error) throw new Error(error.detail ?? m.error_generic());
      if (!data) throw new Error(m.error_generic());
      return data.lines ?? [];
    },
  });
}

export function useImportItems() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { items: { scryfallId: string; quantity: number }[] }) => {
      const { data, error } = await client.POST("/api/collection/import", {
        body: { items: vars.items },
      });
      if (error) throw new Error(error.detail ?? m.error_generic());
      if (!data) throw new Error(m.error_generic());
      return { addedRows: data.addedRows, updatedRows: data.updatedRows };
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["collection"] }),
  });
}

export function useWantlist(cubeId: string) {
  return useQuery({
    queryKey: ["collection", "wantlist", cubeId],
    retry: false,
    queryFn: async () => {
      const { data, error, response } = await client.GET("/api/cubes/{cubeId}/wantlist", {
        params: { path: { cubeId } },
      });
      if (response.status === 401) throw new UnauthorizedError(m.wantlist_login_required());
      if (error) throw new Error(error.detail ?? m.error_generic());
      if (!data) throw new Error(m.error_generic());
      return { cubeName: data.cubeName, items: data.items ?? [], totalMissing: data.totalMissing };
    },
  });
}
