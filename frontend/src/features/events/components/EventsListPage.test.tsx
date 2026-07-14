import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { render, screen } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { EventsListPage } from "./EventsListPage";

afterEach(() => vi.unstubAllGlobals());

function renderPage() {
  const rootRoute = createRootRoute();
  const index = createRoute({
    getParentRoute: () => rootRoute,
    path: "/",
    component: EventsListPage,
  });
  const detail = createRoute({
    getParentRoute: () => rootRoute,
    path: "/events/$eventId",
    component: () => null,
  });
  const create = createRoute({
    getParentRoute: () => rootRoute,
    path: "/events/new",
    component: () => null,
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([index, detail, create]),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
      <RouterProvider router={router as any} />
    </QueryClientProvider>,
  );
}

const eventsPayload = {
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

function stubFetch(role: string) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: Request) => {
      const url = input.url;
      if (url.includes("/api/me")) {
        return new Response(
          JSON.stringify({ id: "u1", email: "x@y", displayName: "X", providers: [], role }),
        );
      }
      return new Response(JSON.stringify(eventsPayload));
    }),
  );
}

test("groups events and shows fee, spots, and my status", async () => {
  stubFetch("user");
  renderPage();
  expect(await screen.findByText("Vintage Night")).toBeInTheDocument();
  expect(screen.getByText("Upcoming")).toBeInTheDocument();
  expect(screen.getByText("Past & cancelled")).toBeInTheDocument();
  expect(screen.getByText(/8\/8 spots/)).toBeInTheDocument();
  // The list summary carries no waitlist position — the badge is generic.
  expect(screen.getByText("Waitlisted")).toBeInTheDocument();
  expect(screen.queryByText("New event")).not.toBeInTheDocument();
});

test("admins see the new-event button", async () => {
  stubFetch("admin");
  renderPage();
  expect(await screen.findByText("New event")).toBeInTheDocument();
});
