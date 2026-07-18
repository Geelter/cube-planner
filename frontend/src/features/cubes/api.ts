import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { m } from "@/paraglide/messages";
import { client } from "@/shared/api/client";
import { unwrap } from "@/shared/api/helpers";
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
      const body = unwrap(data, error);
      return { cubes: body.cubes ?? [], total: body.total };
    },
  });
}

export function useMyCubes() {
  return useQuery({
    queryKey: ["cubes", "mine"],
    queryFn: async () => {
      const { data, error } = await client.GET("/api/me/cubes");
      return unwrap(data, error).cubes ?? [];
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
      return unwrap(data, error);
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
      const body = unwrap(data, error);
      return { cards: body.cards ?? [], version: body.version };
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
      const body = unwrap(data, error);
      return { changes: body.changes ?? [], total: body.total };
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
      return unwrap(data, error);
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
      return unwrap(data, error);
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["cubes"] }),
  });
}

export function useDeleteCube(cubeId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      // 204 No Content on success: no body to unwrap, so the error check
      // stays inline rather than going through unwrap (which would treat
      // the always-absent data as a failure).
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
      return unwrap(data, error);
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["cubes"] }),
  });
}
