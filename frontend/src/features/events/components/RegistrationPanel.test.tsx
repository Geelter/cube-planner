import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import type { EventDetail } from "../api";

const register = vi.fn();
const pay = vi.fn();
const cancel = vi.fn();
const cancelState: { isPending: boolean } = { isPending: false };
vi.mock("../api", async (orig) => ({
  ...(await orig()),
  useRegister: () => ({ mutate: register, isPending: false, error: null }),
  usePay: () => ({ mutate: pay, isPending: false, error: null }),
  useCancelRegistration: () => ({ mutate: cancel, error: null, ...cancelState }),
}));

import { RegistrationPanel } from "./RegistrationPanel";

afterEach(() => {
  cleanup();
  cancel.mockReset();
  cancelState.isPending = false;
});

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
  // Fresh element per (re)render — reusing one element reference makes React
  // bail out of reconciling the subtree, hiding mock-state changes.
  const makeUi = () => (
    <QueryClientProvider client={qc}>
      <RegistrationPanel event={event} checkoutCancelled={checkoutCancelled} />
    </QueryClientProvider>
  );
  const view = render(makeUi());
  return { ...view, rerenderSame: () => view.rerender(makeUi()) };
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

test("free event past deadline → cancel shows plain confirm, no refund footnote", async () => {
  renderPanel(
    baseEvent({
      feeCents: 0,
      refundDeadline: new Date(Date.now() - 3600_000).toISOString(),
      myRegistration: { id: "r1", status: "paid" },
    }),
  );
  expect(screen.queryByText(/Free cancellation until/)).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Cancel registration" }));
  expect(await screen.findByText(/Cancel your registration for Cube Night\?/)).toBeInTheDocument();
  expect(
    screen.queryByText(/only get your money back if the organizer approves/),
  ).not.toBeInTheDocument();
});

test("confirm-cancel keeps the dialog open and spins while the cancellation is pending", async () => {
  // The mocked mutate flips the hook into its pending state (as the real
  // mutation would); rerenderSame lets the component observe it.
  cancel.mockImplementation(() => {
    cancelState.isPending = true;
  });
  const view = renderPanel(
    baseEvent({
      refundDeadline: new Date(Date.now() + 3600_000).toISOString(),
      myRegistration: { id: "r1", status: "paid" },
    }),
  );
  await userEvent.click(screen.getByRole("button", { name: "Cancel registration" }));
  const confirmButtons = screen.getAllByRole("button", { name: "Cancel registration" });
  await userEvent.click(confirmButtons[confirmButtons.length - 1]!);
  view.rerenderSame();
  // The dialog stays open while the mutation is in flight…
  expect(screen.getByText(/Cancel your registration for Cube Night\?/)).toBeInTheDocument();
  // …and its confirm button carries the spinner (match by textContent — the
  // spinner's aria-label joins the accessible name while loading).
  const confirm = screen
    .getAllByRole("button")
    .filter((b) => b.textContent === "Cancel registration")
    .at(-1)!;
  expect(confirm).toHaveAttribute("aria-busy", "true");
});

test("refund_requested → status note, no buttons", () => {
  renderPanel(baseEvent({ myRegistration: { id: "r1", status: "refund_requested" } }));
  expect(screen.getByText(/refund pending organizer review/)).toBeInTheDocument();
  expect(screen.queryByRole("button")).not.toBeInTheDocument();
});
