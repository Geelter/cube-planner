// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { cleanup, render } from "@testing-library/react";
import { axe } from "vitest-axe";
import { afterEach, expect, it, vi } from "vitest";
import { routeTree } from "@/routeTree.gen";

const eventsListPayload = {
  events: [
    {
      id: "e1",
      name: "Vintage Night",
      startsAt: "2026-08-01T18:00:00Z",
      location: "LGS",
      feeCents: 5000,
      currency: "pln",
      maxParticipants: 8,
      paidCount: 8,
      pendingCount: 0,
      waitlistCount: 2,
      status: "published",
      myRegistrationStatus: "waitlisted",
    },
    {
      id: "e2",
      name: "Old Draft Night",
      startsAt: "2026-06-01T18:00:00Z",
      location: "",
      feeCents: 0,
      currency: "pln",
      maxParticipants: 8,
      paidCount: 6,
      pendingCount: 0,
      waitlistCount: 0,
      status: "finished",
    },
  ],
};

function eventDetailPayload() {
  return {
    id: "e1",
    name: "Cube Night",
    startsAt: "2026-08-01T18:00:00Z",
    location: "LGS",
    feeCents: 5000,
    currency: "pln",
    maxParticipants: 8,
    paidCount: 0,
    pendingCount: 1,
    waitlistCount: 0,
    status: "published",
    description: "A friendly cube draft night.",
    organizerName: "Org",
    cubes: [],
    attendees: [],
    myRegistration: {
      id: "r1",
      status: "pending_payment",
      expiresAt: new Date(Date.now() + 15 * 60 * 1000).toISOString(),
    },
  };
}

function manageEventPayload() {
  return {
    id: "e1",
    name: "Draft Cube Night",
    startsAt: "2026-09-01T18:00:00Z",
    location: "LGS",
    feeCents: 5000,
    currency: "pln",
    maxParticipants: 8,
    paidCount: 0,
    pendingCount: 0,
    waitlistCount: 0,
    status: "draft",
    description: "",
    organizerName: "Org",
    cubes: [],
    attendees: [],
  };
}

function meResponse(role: string) {
  return new Response(
    JSON.stringify({ id: "u1", email: "x@y", displayName: "X", providers: [], role }),
    { status: 200, headers: { "Content-Type": "application/json" } },
  );
}

function jsonResponse(body: unknown) {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

async function renderRoute(path: string) {
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: [path] }),
  });
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const { container } = render(
    <QueryClientProvider client={qc}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  await router.load();
  return container;
}

it("/events has no axe violations", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: Request | string) => {
      const url = typeof input === "string" ? input : input.url;
      if (url.includes("/api/me/cubes")) return jsonResponse({ cubes: [] });
      if (url.includes("/api/me")) return meResponse("user");
      if (url.includes("/api/events")) return jsonResponse(eventsListPayload);
      return new Response("{}", { status: 401 });
    }),
  );
  vi.stubEnv("DEV", false);

  expect(await axe(await renderRoute("/events"))).toHaveNoViolations();
});

it("/events/$eventId has no axe violations", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: Request | string) => {
      const url = typeof input === "string" ? input : input.url;
      if (url.includes("/api/me/cubes")) return jsonResponse({ cubes: [] });
      if (url.includes("/api/me")) return meResponse("user");
      if (url.includes("/api/events/e1")) return jsonResponse(eventDetailPayload());
      if (url.includes("/api/events")) return jsonResponse(eventsListPayload);
      return new Response("{}", { status: 401 });
    }),
  );
  vi.stubEnv("DEV", false);

  expect(await axe(await renderRoute("/events/e1"))).toHaveNoViolations();
});

it("/events/$eventId/manage has no axe violations", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: Request | string) => {
      const url = typeof input === "string" ? input : input.url;
      if (url.includes("/api/events/e1/registrations")) return jsonResponse({ registrations: [] });
      if (url.includes("/api/events/e1")) return jsonResponse(manageEventPayload());
      if (url.includes("/api/me/cubes")) return jsonResponse({ cubes: [] });
      if (url.includes("/api/cubes")) return jsonResponse({ cubes: [], total: 0 });
      if (url.includes("/api/me")) return meResponse("admin");
      return new Response("{}", { status: 401 });
    }),
  );
  vi.stubEnv("DEV", false);

  expect(await axe(await renderRoute("/events/e1/manage"))).toHaveNoViolations();
});
