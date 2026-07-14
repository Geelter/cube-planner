import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import type { EventDetail } from "../api";

const mutate = vi.fn();
vi.mock("../api", async (orig) => ({
  ...(await orig()),
  useSetEventCubes: () => ({ mutate, error: null }),
  useLinkableCubes: () => ({ data: [] }),
  useCubeChangelog: () => ({ data: [] }),
}));

import { EventCubesEditor } from "./EventCubesEditor";

afterEach(cleanup);

function baseEvent(overrides: Partial<EventDetail>): EventDetail {
  return {
    id: "e1",
    name: "Cube Night",
    startsAt: "2026-08-01T18:00:00Z",
    location: "LGS",
    feeCents: 5000,
    currency: "pln",
    maxParticipants: 2,
    paidCount: 0,
    pendingCount: 0,
    waitlistCount: 0,
    status: "draft",
    description: "",
    organizerName: "Org",
    cubes: [{ cubeId: "c1", cubeName: "Vintage Cube" }],
    attendees: [],
    ...overrides,
  } as EventDetail;
}

function renderEditor(event: EventDetail) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <EventCubesEditor event={event} />
    </QueryClientProvider>,
  );
}

test("failed remove reverts optimistic update, keeping the cube in the list", async () => {
  mutate.mockImplementation((_vars, opts?: { onError?: () => void }) => {
    opts?.onError?.();
  });
  renderEditor(baseEvent({}));

  expect(screen.getByText("Vintage Cube")).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Remove" }));

  expect(await screen.findByText("Vintage Cube")).toBeInTheDocument();
});
