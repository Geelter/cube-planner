import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { m } from "@/paraglide/messages";
import { client } from "@/shared/api/client";
import type { components } from "@/shared/api/schema";

export type CubeSummary = components["schemas"]["CubeSummary"];
export type CubeDetail = components["schemas"]["CubeDetail"];
export type CubeCardEntry = components["schemas"]["CubeCardEntry"];
export type ChangelogEntry = components["schemas"]["ChangelogEntry"];

export const CUBES_PAGE_SIZE = 20;

/** Thrown by useCommitChange on 409 so the editor can branch on it. */
export class CommitConflictError extends Error {}

export function useCubeList(q: string, page: number) {
  const query = q.trim();
  return useQuery({
    queryKey: ["cubes", "list", query, page],
    placeholderData: keepPreviousData,
    queryFn: async () => {
      const { data, error } = await client.GET("/api/cubes", {
        params: {
          query: { q: query, limit: CUBES_PAGE_SIZE, offset: page * CUBES_PAGE_SIZE },
        },
      });
      if (error) throw new Error(error.detail ?? m.error_generic());
      if (!data) throw new Error(m.error_generic());
      return { cubes: data.cubes ?? [], total: data.total };
    },
  });
}

export function useMyCubes() {
  return useQuery({
    queryKey: ["cubes", "mine"],
    queryFn: async () => {
      const { data, error } = await client.GET("/api/me/cubes");
      if (error) throw new Error(error.detail ?? m.error_generic());
      if (!data) throw new Error(m.error_generic());
      return data.cubes ?? [];
    },
  });
}

export function useCube(cubeId: string) {
  return useQuery({
    queryKey: ["cubes", cubeId],
    queryFn: async (): Promise<CubeDetail> => {
      const { data, error } = await client.GET("/api/cubes/{cubeId}", {
        params: { path: { cubeId } },
      });
      if (error) throw new Error(error.detail ?? m.error_generic());
      if (!data) throw new Error(m.error_generic());
      return data;
    },
  });
}

/** atVersion undefined = current list. */
export function useCubeCards(cubeId: string, atVersion?: number) {
  return useQuery({
    queryKey: ["cubes", cubeId, "cards", atVersion ?? -1],
    queryFn: async () => {
      const { data, error } = await client.GET("/api/cubes/{cubeId}/cards", {
        params: { path: { cubeId }, query: { atVersion: atVersion ?? -1 } },
      });
      if (error) throw new Error(error.detail ?? m.error_generic());
      if (!data) throw new Error(m.error_generic());
      return { cards: data.cards ?? [], version: data.version };
    },
  });
}

export function useCubeChanges(cubeId: string, page: number) {
  return useQuery({
    queryKey: ["cubes", cubeId, "changes", page],
    placeholderData: keepPreviousData,
    queryFn: async () => {
      const { data, error } = await client.GET("/api/cubes/{cubeId}/changes", {
        params: {
          path: { cubeId },
          query: { limit: CUBES_PAGE_SIZE, offset: page * CUBES_PAGE_SIZE },
        },
      });
      if (error) throw new Error(error.detail ?? m.error_generic());
      if (!data) throw new Error(m.error_generic());
      return { changes: data.changes ?? [], total: data.total };
    },
  });
}

export function useCreateCube() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (body: {
      name: string;
      description?: string;
      visibility: "public" | "private";
    }): Promise<CubeDetail> => {
      const { data, error } = await client.POST("/api/cubes", { body });
      if (error) throw new Error(error.detail ?? m.error_generic());
      if (!data) throw new Error(m.error_generic());
      return data;
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["cubes"] }),
  });
}

export function useUpdateCube(cubeId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (body: {
      name?: string;
      description?: string;
      visibility?: "public" | "private";
    }): Promise<CubeDetail> => {
      const { data, error } = await client.PATCH("/api/cubes/{cubeId}", {
        params: { path: { cubeId } },
        body,
      });
      if (error) throw new Error(error.detail ?? m.error_generic());
      if (!data) throw new Error(m.error_generic());
      return data;
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["cubes"] }),
  });
}

export function useDeleteCube(cubeId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      const { error } = await client.DELETE("/api/cubes/{cubeId}", {
        params: { path: { cubeId } },
      });
      if (error) throw new Error(error.detail ?? m.error_generic());
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["cubes"] }),
  });
}

export type CommitInput = {
  expectedVersion: number;
  note?: string;
  adds?: { scryfallId: string; quantity: number }[];
  removes?: { oracleId: string; quantity: number }[];
};

export function useCommitChange(cubeId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (body: CommitInput) => {
      const { data, error, response } = await client.POST("/api/cubes/{cubeId}/changes", {
        params: { path: { cubeId } },
        body,
      });
      if (response.status === 409) throw new CommitConflictError(m.cubes_conflict_toast());
      if (error) throw new Error(error.detail ?? m.error_generic());
      if (!data) throw new Error(m.error_generic());
      return data;
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["cubes"] }),
  });
}
