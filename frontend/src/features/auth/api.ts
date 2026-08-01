import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { client } from "@/shared/api/client";
import { unwrap } from "@/shared/api/helpers";
import type { components } from "@/shared/api/schema";
import { m } from "@/paraglide/messages";

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
  const navigate = useNavigate();
  return useMutation({
    mutationFn: async () => {
      await client.POST("/api/auth/logout");
    },
    onSuccess: async () => {
      // Navigate first so user-scoped queries on the current page unmount
      // instead of refetching into 401s; then invalidate everything so no
      // stale user data (me, collection, my cubes) survives the session.
      await navigate({ to: "/login" });
      void qc.invalidateQueries();
    },
  });
}

export function useRegister() {
  return useMutation({
    mutationFn: async (body: { email: string; displayName: string; password: string }) => {
      const { error } = await client.POST("/api/auth/register", { body });
      if (error) throw new Error(error.detail ?? m.error_generic());
    },
  });
}

export function useForgotPassword() {
  return useMutation({
    mutationFn: async (body: { email: string }) => {
      // Anti-enumeration: succeed regardless of the response so the page
      // shows "sent" either way (matches previous page behavior).
      await client.POST("/api/auth/forgot-password", { body });
    },
  });
}

export function useResetPassword() {
  return useMutation({
    mutationFn: async (body: { token: string; newPassword: string }) => {
      const { error } = await client.POST("/api/auth/reset-password", { body });
      if (error) throw new Error(error.detail ?? m.error_generic());
    },
  });
}
