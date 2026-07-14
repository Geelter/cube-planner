import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { EventDetailPage } from "./EventDetailPage";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function renderAt(path: string) {
  const rootRoute = createRootRoute();
  const detail = createRoute({
    getParentRoute: () => rootRoute,
    path: "/events/$eventId/",
    component: EventDetailPage,
    validateSearch: (s: Record<string, unknown>): { checkout?: "success" | "cancelled" } => ({
      ...(s.checkout === "success" || s.checkout === "cancelled" ? { checkout: s.checkout } : {}),
    }),
  });
  const manage = createRoute({
    getParentRoute: () => rootRoute,
    path: "/events/$eventId/manage",
    component: () => null,
  });
  const cube = createRoute({
    getParentRoute: () => rootRoute,
    path: "/cubes/$cubeId",
    component: () => null,
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([detail, manage, cube]),
    history: createMemoryHistory({ initialEntries: [path] }),
  });
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
      <RouterProvider router={router as any} />
    </QueryClientProvider>,
  );
}

function eventDetailPayload(status: "pending_payment" | "paid") {
  return {
    id: "e1",
    name: "Cube Night",
    startsAt: "2026-08-01T18:00:00Z",
    location: "LGS",
    feeCents: 5000,
    currency: "pln",
    maxParticipants: 8,
    paidCount: status === "paid" ? 1 : 0,
    pendingCount: status === "pending_payment" ? 1 : 0,
    waitlistCount: 0,
    status: "published",
    description: "",
    organizerName: "Org",
    cubes: [],
    attendees: [],
    myRegistration: { id: "r1", status },
  };
}

function stubDetailFetch(status: "pending_payment" | "paid") {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: Request) => {
      const url = input.url;
      if (url.includes("/api/me")) {
        return new Response(
          JSON.stringify({ id: "u1", email: "x@y", displayName: "X", providers: [], role: "user" }),
        );
      }
      return new Response(JSON.stringify(eventDetailPayload(status)));
    }),
  );
}

test("checkout=success with a pending registration shows the confirming panel", async () => {
  stubDetailFetch("pending_payment");
  renderAt("/events/e1?checkout=success");
  expect(await screen.findByText("Confirming your payment…")).toBeInTheDocument();
});

test("checkout=success with a paid registration renders server truth", async () => {
  stubDetailFetch("paid");
  renderAt("/events/e1?checkout=success");
  expect(await screen.findByText("You're in")).toBeInTheDocument();
  expect(screen.queryByText("Confirming your payment…")).not.toBeInTheDocument();
});
