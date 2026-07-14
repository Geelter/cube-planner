import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import type { EventDetail } from "../api";

const register = vi.fn();
const pay = vi.fn();
const cancel = vi.fn();
vi.mock("../api", async (orig) => ({
  ...(await orig()),
  useRegister: () => ({ mutate: register, isPending: false, error: null }),
  usePay: () => ({ mutate: pay, isPending: false, error: null }),
  useCancelRegistration: () => ({ mutate: cancel, isPending: false, error: null }),
}));

import { RegistrationPanel } from "./RegistrationPanel";

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
    status: "published",
    description: "",
    organizerName: "Org",
    cubes: [],
    attendees: [],
    ...overrides,
  } as EventDetail;
}

function renderPanel(event: EventDetail, checkoutCancelled = false) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <RegistrationPanel event={event} checkoutCancelled={checkoutCancelled} />
    </QueryClientProvider>,
  );
}

test("no registration, spots free → Register", async () => {
  renderPanel(baseEvent({}));
  await userEvent.click(screen.getByRole("button", { name: "Register" }));
  expect(register).toHaveBeenCalled();
});

test("no registration, event full → Join the waitlist", () => {
  renderPanel(baseEvent({ paidCount: 2 }));
  expect(screen.getByRole("button", { name: "Join the waitlist" })).toBeInTheDocument();
});

test("pending payment → Pay now + countdown", () => {
  renderPanel(
    baseEvent({
      myRegistration: {
        id: "r1",
        status: "pending_payment",
        expiresAt: new Date(Date.now() + 3 * 3600_000).toISOString(),
      },
    }),
  );
  expect(screen.getByRole("button", { name: "Pay now" })).toBeInTheDocument();
  expect(screen.getByText(/Time left to pay/)).toBeInTheDocument();
});

test("paid past refund deadline → cancel warns about losing money", async () => {
  renderPanel(
    baseEvent({
      refundDeadline: new Date(Date.now() - 3600_000).toISOString(),
      myRegistration: { id: "r1", status: "paid" },
    }),
  );
  await userEvent.click(screen.getByRole("button", { name: "Cancel registration" }));
  expect(
    await screen.findByText(/only get your money back if the organizer approves/),
  ).toBeInTheDocument();
});

test("refund_requested → status note, no buttons", () => {
  renderPanel(baseEvent({ myRegistration: { id: "r1", status: "refund_requested" } }));
  expect(screen.getByText(/refund pending organizer review/)).toBeInTheDocument();
  expect(screen.queryByRole("button")).not.toBeInTheDocument();
});
