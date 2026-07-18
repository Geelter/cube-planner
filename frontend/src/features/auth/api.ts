import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { client } from "@/shared/api/client";
import { unwrap } from "@/shared/api/helpers";
import type { components } from "@/shared/api/schema";

export type User = components["schemas"]["UserBody"];

export function useMe() {
  return useQuery({
    queryKey: ["me"],
    retry: false,
    queryFn: async (): Promise<User | null> => {
      const { data, response } = await client.GET("/api/me");
      if (response.status === 401) return null;
      if (!data) throw new Error("failed to load current user");
      return data;
    },
  });
}

export function useLogin() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (body: { email: string; password: string }) => {
      const { data, error } = await client.POST("/api/auth/login", { body });
      return unwrap(data, error);
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["me"] }),
  });
}

export function useLogout() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      await client.POST("/api/auth/logout");
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["me"] }),
  });
}
