import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { m } from "@/paraglide/messages";
import { client } from "@/shared/api/client";
import type { components } from "@/shared/api/schema";

export type EventSummary = components["schemas"]["EventSummary"];
export type EventDetail = components["schemas"]["EventDetailBody"];
export type EventCubeEntry = components["schemas"]["EventCubeEntry"];
export type RegistrationInfo = components["schemas"]["RegistrationInfo"];
export type EventRegistrationRow = components["schemas"]["EventRegistrationRow"];

/** Thrown on 401 so pages can render a login prompt instead of a generic error. */
export class UnauthorizedError extends Error {}

function unwrap<T>(data: T | undefined, error: { detail?: string | null } | undefined): T {
  if (error) throw new Error(error.detail ?? m.error_generic());
  if (!data) throw new Error(m.error_generic());
  return data;
}

export function useEvents() {
  return useQuery({
    queryKey: ["events", "list"],
    retry: false,
    queryFn: async () => {
      const { data, error, response } = await client.GET("/api/events");
      if (response.status === 401) throw new UnauthorizedError(m.events_login_required());
      return unwrap(data, error).events ?? [];
    },
  });
}

export function useEvent(eventId: string, opts?: { refetchInterval?: number | false }) {
  return useQuery({
    queryKey: ["events", "detail", eventId],
    retry: false,
    refetchInterval: opts?.refetchInterval ?? false,
    queryFn: async () => {
      const { data, error, response } = await client.GET("/api/events/{eventId}", {
        params: { path: { eventId } },
      });
      if (response.status === 401) throw new UnauthorizedError(m.events_login_required());
      return unwrap(data, error);
    },
  });
}

function useEventMutation<TVars, TData>(fn: (vars: TVars) => Promise<TData>) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["events"] }),
  });
}

export function useRegister(eventId: string) {
  return useEventMutation(async () => {
    const { data, error } = await client.POST("/api/events/{eventId}/register", {
      params: { path: { eventId } },
    });
    return unwrap(data, error);
  });
}

export function usePay(eventId: string) {
  // No invalidation: on success the browser leaves for Stripe Checkout.
  return useMutation({
    mutationFn: async () => {
      const { data, error } = await client.POST("/api/events/{eventId}/registration/pay", {
        params: { path: { eventId } },
      });
      return unwrap(data, error).checkoutUrl;
    },
    onSuccess: (url) => window.location.assign(url),
  });
}

export function useCancelRegistration(eventId: string) {
  return useEventMutation(async () => {
    const { data, error } = await client.DELETE("/api/events/{eventId}/registration", {
      params: { path: { eventId } },
    });
    return unwrap(data, error);
  });
}

// ---- organizer ----

export type EventFormValues = {
  name: string;
  description: string;
  location: string;
  startsAt: string; // RFC3339
  feeCents: number;
  maxParticipants: number;
  refundDeadline?: string;
};

export function useCreateEvent() {
  return useEventMutation(async (body: EventFormValues) => {
    const { data, error } = await client.POST("/api/events", { body });
    return unwrap(data, error);
  });
}

export function useUpdateEvent(eventId: string) {
  return useEventMutation(async (body: Partial<EventFormValues>) => {
    const { data, error } = await client.PATCH("/api/events/{eventId}", {
      params: { path: { eventId } },
      body,
    });
    return unwrap(data, error);
  });
}

export type EventAction = "publish" | "start" | "finish" | "cancel";

const ACTION_PATHS = {
  publish: "/api/events/{eventId}/publish",
  start: "/api/events/{eventId}/start",
  finish: "/api/events/{eventId}/finish",
  cancel: "/api/events/{eventId}/cancel",
} as const;

export function useEventAction(eventId: string) {
  return useEventMutation(async (action: EventAction) => {
    const { data, error } = await client.POST(ACTION_PATHS[action], {
      params: { path: { eventId } },
    });
    return unwrap(data, error);
  });
}

export function useSetEventCubes(eventId: string) {
  return useEventMutation(async (cubes: { cubeId: string; cubeChangeId?: string }[]) => {
    const { data, error } = await client.PUT("/api/events/{eventId}/cubes", {
      params: { path: { eventId } },
      body: { cubes },
    });
    return unwrap(data, error);
  });
}

export function useEventRegistrations(eventId: string) {
  return useQuery({
    queryKey: ["events", "registrations", eventId],
    retry: false,
    queryFn: async () => {
      const { data, error } = await client.GET("/api/events/{eventId}/registrations", {
        params: { path: { eventId } },
      });
      return unwrap(data, error).registrations ?? [];
    },
  });
}

export function useRefundRegistration(eventId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (registrationId: string) => {
      const { data, error } = await client.POST(
        "/api/events/{eventId}/registrations/{registrationId}/refund",
        { params: { path: { eventId, registrationId } } },
      );
      return unwrap(data, error);
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["events"] }),
  });
}

export function useDenyRefund(eventId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (registrationId: string) => {
      const { data, error } = await client.POST(
        "/api/events/{eventId}/registrations/{registrationId}/deny-refund",
        { params: { path: { eventId, registrationId } } },
      );
      return unwrap(data, error);
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["events"] }),
  });
}

// Cube linking sources. features must not import other features
// (structure.md), so events talks to the cubes API through the shared
// generated client directly.
export function useLinkableCubes() {
  return useQuery({
    queryKey: ["events", "linkable-cubes"],
    retry: false,
    queryFn: async () => {
      const [pub, mine] = await Promise.all([
        client.GET("/api/cubes", { params: { query: { limit: 100, offset: 0 } } }),
        client.GET("/api/me/cubes"),
      ]);
      const cubes = [...(pub.data?.cubes ?? []), ...(mine.data?.cubes ?? [])];
      const seen = new Set<string>();
      return cubes.filter((c) => (seen.has(c.id) ? false : (seen.add(c.id), true)));
    },
  });
}

export function useCubeChangelog(cubeId: string | null) {
  return useQuery({
    queryKey: ["events", "cube-changes", cubeId],
    enabled: cubeId != null,
    retry: false,
    queryFn: async () => {
      const { data, error } = await client.GET("/api/cubes/{cubeId}/changes", {
        params: { path: { cubeId: cubeId! }, query: { limit: 50, offset: 0 } },
      });
      return unwrap(data, error).changes ?? [];
    },
  });
}
